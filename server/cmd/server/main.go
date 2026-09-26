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

	"gin-sqlite-example/internal/config"
	"gin-sqlite-example/internal/handler"
	"gin-sqlite-example/internal/middleware"
	"gin-sqlite-example/internal/repository"
	"gin-sqlite-example/internal/service"
)

func main() {
	cfg := config.Load()
	gin.SetMode(gin.ReleaseMode)

	// ---- 依赖组装：repository -> service -> handler（手工 DI，不引入 wire/fx）----
	db, err := repository.NewDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	deviceRepo := repository.NewDeviceRepository(db)
	deviceSvc := service.NewDeviceService(deviceRepo)
	deviceHandler := handler.NewDeviceHandler(deviceSvc)

	router := setupRouter(deviceHandler)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %s", err)
	}
	log.Println("server exited")
}

func setupRouter(h *handler.DeviceHandler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestLogger(), middleware.RateLimit(20, 40))

	r.GET("/healthz", func(c *gin.Context) {
		handler.Success(c, gin.H{"status": "up"})
	})

	v1 := r.Group("/api/v1")
	{
		devices := v1.Group("/devices")
		devices.POST("", h.Create)
		devices.GET("", h.List)
		devices.GET("/:id", h.Get)
		devices.PATCH("/:id", h.Update)
		devices.DELETE("/:id", h.Delete)
	}
	return r
}
