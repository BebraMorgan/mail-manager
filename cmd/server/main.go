package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"mail-manager/internal/config"
	"mail-manager/internal/llm"
	"mail-manager/internal/mail"
	"mail-manager/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	db, err := storage.Open(ctx, cfg.Database.Path)
	if err != nil {
		slog.Error("database error", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		slog.Error("migration error", "error", err)
		return
	}

	if err := db.RecoverJobs(ctx); err != nil {
		slog.Error("queue recovery error", "error", err)
		return
	}

	classifier := llm.New(
		cfg.LLM,
		cfg.Classifier.MaxEmailChars,
	)

	mailClient := mail.New(cfg.Mail)

	worker := mail.NewWorker(
		mailClient,
		db,
		classifier,
	)

	scheduler := mail.NewScheduler(
		mailClient,
		db,
		worker,
	)

	gin.SetMode(cfg.App.Mode)

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "mail-manager",
		})
	})

	router.GET("/health/db", func(c *gin.Context) {
		if err := db.DB.PingContext(
			c.Request.Context(),
		); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unavailable",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"database": "sqlite",
		})
	})

	router.POST("/api/mail/sync", func(c *gin.Context) {
		count, err := scheduler.Scan(c.Request.Context())
		if err != nil {
			slog.Error("manual sync failed", "error", err)

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "IMAP synchronization failed",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":     "ok",
			"registered": count,
		})
	})

	router.GET("/api/reports/items", func(c *gin.Context) {
		items, err := db.GetReportItems(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "database error",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"items": items,
			"count": len(items),
		})
	})

	server := &http.Server{
		Addr: cfg.App.Host + ":" +
			strconv.Itoa(cfg.App.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Scheduler and HTTP API run independently.
	schedulerDone := make(chan struct{})

	go func() {
		defer close(schedulerDone)
		scheduler.Run(ctx, cfg.Mail.PollInterval)
	}()

	go func() {
		slog.Info("HTTP server started", "addr", server.Addr)

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown error", "error", err)
	}

	select {
	case <-schedulerDone:
	case <-shutdownCtx.Done():
		slog.Warn("scheduler shutdown timeout")
	}

	slog.Info("mail-manager stopped")
}
