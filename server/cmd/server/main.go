package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/config"
	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/httpserver"
	"mini-ruoyi/internal/job"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/perm"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

const (
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 15 * time.Second // 必须大于 writeTimeout，否则会掐断进行中的响应
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置: %v", err)
	}
	if cfg.Debug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := repository.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	if err := repository.Migrate(context.Background(), db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := verifyPermCodes(context.Background(), db); err != nil {
		log.Fatalf("权限码校验失败: %v", err)
	}

	// ---- 依赖组装：repository -> service -> handler（手工 DI，不引入 wire/fx）----
	userRepo := repository.NewUserRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	sessionRepo := repository.NewSessionRepository(db)

	sessionSvc := auth.NewSessionService(sessionRepo, userRepo)
	logSvc := service.NewLogService(repository.NewLogRepository(db), cfg.Log.RetentionDays)
	logSvc.Start()
	// 关闭前把缓冲里的日志落库。不调的话最后几秒的日志会丢，
	// 而那恰恰是最可能出问题的时间段。
	defer logSvc.Stop()
	authzSvc := service.NewAuthzService(userRepo, roleRepo)
	userSvc := service.NewUserService(userRepo, sessionSvc)
	roleSvc := service.NewRoleService(roleRepo)
	menuSvc := service.NewMenuService(menuRepo)

	// 定时任务：任务本体在代码里注册，数据库只存开关与 cron 表达式。
	// 定义在 internal/job，主程序与测试 fixture 共用同一套。
	registry := job.NewRegistry()
	registry.Register(job.CleanupExpiredSessions(sessionSvc))
	registry.Register(job.CleanupOldLogs(logSvc))

	monitorSvc := service.NewMonitorService(filepath.Dir(cfg.Database.Path))

	fileSvc, err := service.NewFileService(
		repository.NewFileRepository(db), cfg.Upload.Dir, cfg.UploadMaxBytes(), cfg.UploadQuotaBytes())
	if err != nil {
		log.Fatalf("初始化文件存储: %v", err)
	}
	registry.Register(job.CleanupOrphanFiles(fileSvc))

	jobSvc := service.NewJobService(repository.NewJobRepository(db), registry)
	if err := jobSvc.Start(context.Background()); err != nil {
		log.Fatalf("启动定时任务: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := jobSvc.Stop(ctx); err != nil {
			log.Printf("停止定时任务: %v", err)
		}
	}()

	authMW := middleware.NewAuth(sessionSvc, authzSvc, cfg.Server.SecureCookie)

	// 路由表由 NewRouter 在装配时生成，而权限清单接口本身也要在装配期注册，
	// 所以这里传一个函数延迟取值——真正被调用是在第一个请求进来之后。
	var routes *httpserver.RouteTable
	permHandler := handler.NewPermHandler(func() []handler.RouteInfo {
		if routes == nil {
			return nil
		}
		return routes.PermRoutes()
	})

	router, routes, err := httpserver.NewRouter(httpserver.Deps{
		DB:      db,
		Auth:    handler.NewAuthHandler(sessionSvc, authzSvc, menuSvc, authMW, logSvc),
		Session: handler.NewSessionHandler(sessionSvc),
		Log:     handler.NewLogHandler(logSvc),
		Job:     handler.NewJobHandler(jobSvc),
		File:    handler.NewFileHandler(fileSvc),
		Profile: handler.NewProfileHandler(userSvc),
		Monitor: handler.NewMonitorHandler(monitorSvc),
		OperLog: logSvc,
		User:    handler.NewUserHandler(userSvc),
		Role:    handler.NewRoleHandler(roleSvc),
		Menu:    handler.NewMenuHandler(menuSvc),
		Perm:    permHandler,
		AuthMW:  authMW,
		WebDir:  cfg.Web.Dir,

		TrustedProxies: cfg.Server.TrustedProxies,
		RateLimitRPS:   cfg.Server.RateLimit.RPS,
		RateLimitBurst: cfg.Server.RateLimit.Burst,
	})
	if err != nil {
		log.Fatalf("init router: %v", err)
	}
	log.Printf("已注册 %d 个 API 端点（公开 %d / 仅登录 %d / 需权限 %d）",
		routes.Endpoints(), len(routes.Public), len(routes.Self), len(routes.Protected))
	log.Printf("%s", cfg.Summary(fileSvc.Dir()))

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	go func() {
		log.Printf("server starting on %s", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %s", err)
	}
	log.Println("server exited")
}

// verifyPermCodes 校验 sys_role_perms 里出现的权限码都已在代码中声明。
//
// 出现未知码就拒绝启动：那意味着权限码改过名而库没同步，或有人直接改了库。
// 静默跑下去的结果是「某些人少了权限」或「权限码被复用后某些人多了权限」，
// 两者都不会报错，只能靠人发现。见 docs/schema.md §8.4。
func verifyPermCodes(ctx context.Context, db *sql.DB) error {
	codes, err := repository.NewRoleRepository(db).ListPermCodes(ctx)
	if err != nil {
		return fmt.Errorf("读取已授予的权限码: %w", err)
	}
	if len(codes) == 0 {
		return nil
	}
	return perm.Validate(codes)
}
