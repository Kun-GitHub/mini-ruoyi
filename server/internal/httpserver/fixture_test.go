package httpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/job"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

// RecoveryForTest 暴露中间件，供 panic 用例构造最小路由。
func RecoveryForTest() gin.HandlerFunc { return middleware.Recovery() }

// testDeps 组装一套可直接用于测试的依赖，数据库是临时库并已跑完迁移与种子。
//
// 必须与 cmd/server/main.go 的组装保持一致：少构造一个 handler，
// 对应的 Deps 字段就是 nil，请求会 panic（被 Recovery 兜成 500，
// 表现为一堆莫名其妙的 500 而不是明确的错误）。
func testDeps(t *testing.T, webDir string) Deps {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	userRepo := repository.NewUserRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	sessionRepo := repository.NewSessionRepository(db)

	sessionSvc := auth.NewSessionService(sessionRepo, userRepo)
	logSvc := service.NewLogService(repository.NewLogRepository(db), 30)
	logSvc.Start()
	t.Cleanup(logSvc.Stop)
	testLogSvc = logSvc
	authzSvc := service.NewAuthzService(userRepo, roleRepo)
	userSvc := service.NewUserService(userRepo, sessionSvc)
	roleSvc := service.NewRoleService(roleRepo)
	menuSvc := service.NewMenuService(menuRepo)

	// 与 main.go 用同一套任务定义，避免两边漂移
	registry := job.NewRegistry()
	registry.Register(job.CleanupExpiredSessions(sessionSvc))
	registry.Register(job.CleanupOldLogs(logSvc))

	fileSvc, err := service.NewFileService(
		repository.NewFileRepository(db), filepath.Join(t.TempDir(), "uploads"), 1<<20, 4<<20)
	if err != nil {
		t.Fatalf("初始化文件存储: %v", err)
	}
	registry.Register(job.CleanupOrphanFiles(fileSvc))

	jobSvc := service.NewJobService(repository.NewJobRepository(db), registry)
	if err := jobSvc.Start(t.Context()); err != nil {
		t.Fatalf("启动定时任务: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = jobSvc.Stop(ctx)
	})

	authMW := middleware.NewAuth(sessionSvc, authzSvc, false)

	return Deps{
		DB:      db,
		Perm:    handler.NewPermHandler(testPermRoutes),
		Auth:    handler.NewAuthHandler(sessionSvc, authzSvc, menuSvc, authMW, logSvc),
		Session: handler.NewSessionHandler(sessionSvc),
		Log:     handler.NewLogHandler(logSvc),
		Job:     handler.NewJobHandler(jobSvc),
		File:    handler.NewFileHandler(fileSvc),
		OperLog: logSvc,
		User:    handler.NewUserHandler(userSvc),
		Role:    handler.NewRoleHandler(roleSvc),
		Menu:    handler.NewMenuHandler(menuSvc),
		AuthMW:  authMW,
		WebDir:  webDir,
	}
}

// testRouteTable 是测试专用的延迟绑定槽位。
//
// 生产代码里对应的是 main.go 的一个局部变量。之所以不把它塞进 Deps：
// Deps.validate() 会把 nil 指针当成「装配遗漏」而拒绝启动，而测试钩子
// 在装配期确实是 nil —— 那正是它的正常状态。
//
// 测试不并行执行，所以这个包级变量不会竞争。
var testRouteTable *RouteTable

// testLogSvc 让用例能显式刷盘。日志是批量落库的，
// 不刷的话每个断言都要等 2 秒的刷盘间隔。
var testLogSvc *service.LogService

func testPermRoutes() []handler.RouteInfo {
	if testRouteTable == nil {
		return nil
	}
	return testRouteTable.PermRoutes()
}

// testWebDir 造一个最小可用的前端产物目录。
func testWebDir(t *testing.T) string {
	t.Helper()

	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>mini-ruoyi</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return webDir
}

// newTestRouter 建一个带最小前端产物目录的路由，返回路由表。
func newTestRouter(t *testing.T) (*gin.Engine, *RouteTable) {
	t.Helper()

	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	// 完成延迟绑定：生产代码里这一步是 main.go 的局部变量赋值
	testRouteTable = table
	return r, table
}
