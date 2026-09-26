package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/config"
	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/httpserver"
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
	cfg := config.Load()
	if cfg.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// ---- 依赖组装：repository -> service -> handler（手工 DI，不引入 wire/fx）----
	db, err := repository.NewDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	if err := repository.Migrate(context.Background(), db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	deviceRepo := repository.NewDeviceRepository(db)
	deviceSvc := service.NewDeviceService(deviceRepo)
	deviceHandler := handler.NewDeviceHandler(deviceSvc)

	router, err := httpserver.NewRouter(deviceHandler, db, cfg.WebDir)
	if err != nil {
		log.Fatalf("init router: %v", err)
	}

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	go func() {
		log.Printf("server starting on %s", cfg.Addr)
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
