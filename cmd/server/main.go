package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"mail-manager/internal/api"
	"mail-manager/internal/auth"
	"mail-manager/internal/config"
	"mail-manager/internal/llm"
	"mail-manager/internal/mail"
	"mail-manager/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := storage.Open(ctx, cfg.Database.Path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		return err
	}
	if recovered, err := db.RecoverMoveReservations(ctx); err != nil {
		return err
	} else if recovered > 0 {
		slog.Warn("interrupted mail moves require human review", "count", recovered)
	}
	if err = db.RecoverJobs(ctx); err != nil {
		return err
	}
	tokens, err := auth.New(cfg.Auth.JWTSecret, cfg.Auth.JWTIssuer, cfg.Auth.TokenTTL)
	if err != nil {
		return err
	}
	llmClient := llm.New(cfg.LLM, cfg.Classifier.MaxEmailChars)
	imapClient := mail.New(cfg.Mail)
	worker := mail.NewWorker(imapClient, db, llmClient)
	scheduler := mail.NewScheduler(imapClient, db, worker)
	gin.SetMode(cfg.App.Mode)
	router := gin.New()
	router.Use(gin.Recovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		return err
	}
	api.Register(router, db, scheduler, api.NewAPIAuth(db, tokens))
	server := &http.Server{
		Addr: net.JoinHostPort(cfg.App.Host, strconv.Itoa(cfg.App.Port)), Handler: router,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	schedulerDone := make(chan struct{})
	go func() { defer close(schedulerDone); scheduler.Run(ctx, cfg.Mail.PollInterval) }()
	serverErr := make(chan error, 1)
	go func() { slog.Info("HTTP listening", "addr", server.Addr); serverErr <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
			return err
		}
		stop()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	select {
	case <-schedulerDone:
	case <-shutdownCtx.Done():
		slog.Warn("scheduler did not exit before timeout")
	}
	return nil
}
