package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"mail-manager/internal/config"
	"mail-manager/internal/mail"
	"mail-manager/internal/storage"
)

func Register(r *gin.Engine, db *storage.Storage, scheduler *mail.Scheduler, security *APIAuth) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok", "service": "mail-manager"}) })
	r.POST("/api/auth/login", security.Login)
	r.POST("/api/auth/refresh", security.Refresh)
	r.POST("/api/auth/logout", security.LogoutWithRefresh)
	secured := r.Group("/api")
	secured.Use(security.Require())
	secured.GET("/auth/me", security.Me)

	secured.POST("/auth/password", security.ChangePassword)
	NewMonitoringHandler(db).Register(secured)

	secured.GET("/health/db", func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := db.DB.PingContext(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ok", "database": "sqlite"})
	})
	secured.GET("/mail/:id/content", NewMailOps(db, scheduler.Client).Content)
	admin := secured.Group("")
	admin.GET("/mail/settings", func(c *gin.Context) {
		cfg, ok := scheduler.Client.Settings()
		if !ok {
			c.JSON(200, gin.H{"configured": false})
			return
		}
		c.JSON(200, gin.H{"configured": true, "host": cfg.Host, "port": cfg.Port, "username": cfg.Username, "folder": cfg.Folder, "trash_folder": cfg.TrashFolder, "allow_delete": cfg.AllowDelete, "auth_method": cfg.AuthMethod, "poll_interval_seconds": int(cfg.PollInterval.Seconds()), "fetch_limit": cfg.FetchLimit, "max_message_bytes": cfg.MaxMessageBytes})
	})
	admin.POST("/mail/settings", func(c *gin.Context) {
		var in struct {
			Host                string `json:"host"`
			Port                int    `json:"port"`
			Username            string `json:"username"`
			AuthMethod          string `json:"auth_method"`
			OAuthToken          string `json:"oauth_token"`
			AppPassword         string `json:"app_password"`
			Folder              string `json:"folder"`
			TrashFolder         string `json:"trash_folder"`
			AllowDelete         bool   `json:"allow_delete"`
			PollIntervalSeconds int    `json:"poll_interval_seconds"`
			FetchLimit          int    `json:"fetch_limit"`
			MaxMessageBytes     int    `json:"max_message_bytes"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "invalid JSON"})
			return
		}
		old, has := scheduler.Client.Settings()
		if has && in.OAuthToken == "" && in.AuthMethod == "oauth2" && in.Username == old.Username && in.Host == old.Host {
			in.OAuthToken = old.OAuthToken
		}
		if has && in.AppPassword == "" && in.AuthMethod == "app_password" && in.Username == old.Username && in.Host == old.Host {
			in.AppPassword = old.AppPassword
		}
		cfg := config.MailConfig{Host: in.Host, Port: in.Port, Username: in.Username, AuthMethod: in.AuthMethod, OAuthToken: in.OAuthToken, AppPassword: in.AppPassword, Folder: in.Folder, TrashFolder: in.TrashFolder, AllowDelete: in.AllowDelete, PollInterval: time.Duration(in.PollIntervalSeconds) * time.Second, FetchLimit: in.FetchLimit, MaxMessageBytes: in.MaxMessageBytes}
		if err := db.SaveMailSettings(c.Request.Context(), cfg); err != nil {
			c.JSON(400, gin.H{"error": "invalid mail settings or encryption configuration"})
			return
		}
		scheduler.Client.Configure(cfg)
		c.JSON(200, gin.H{"status": "ok"})
	})
	admin.POST("/mail/sync", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Minute)
		defer cancel()
		n, err := scheduler.Scan(ctx)
		if errors.Is(err, mail.ErrScanInProgress) {
			c.JSON(409, gin.H{"error": "scan already in progress"})
			return
		}
		if err != nil {
			slog.Warn("manual IMAP scan failed", "error", err)
			c.JSON(503, gin.H{"error": "IMAP synchronization failed"})
			return
		}
		c.JSON(200, gin.H{"registered": n, "status": "ok"})
	})
	mailOps := NewMailOps(db, scheduler.Client)
	admin.POST("/mail/delete", mailOps.MoveToTrash)
	admin.POST("/mail/delete/preview", mailOps.Preview)
	admin.GET("/mail/folders", mailOps.Folders)
	admin.GET("/mail/delete-audit", mailOps.Audit)
	admin.POST("/mail/delete/:id/reconcile", mailOps.Reconcile)
	classificationOps := NewClassificationOps(db)
	admin.PATCH("/classifications/:id", classificationOps.Review)
	admin.POST("/classifications/:id/retry", classificationOps.Requeue)

}
