package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"mail-manager/internal/mail"
	"mail-manager/internal/storage"
)

func Register(r *gin.Engine, db *storage.Storage, scheduler *mail.Scheduler, security *APIAuth) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok", "service": "mail-manager"}) })
	r.POST("/api/auth/login", security.Login)
	secured := r.Group("/api")
	secured.Use(security.Require())
	secured.GET("/auth/me", security.Me)
	secured.POST("/auth/password", security.ChangePassword)
	NewMonitoringHandler(db).Register(secured)
	secured.GET("/reports/items", func(c *gin.Context) {
		items, err := db.GetReportItems(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": "database error"})
			return
		}
		c.JSON(200, gin.H{"count": len(items), "items": items})
	})
	secured.GET("/health/db", func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := db.DB.PingContext(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ok", "database": "sqlite"})
	})
	secured.GET("/mail/:id/content", NewMailOps(db, scheduler.Client, scheduler.Client.PublicSettings()).Content)
	admin := secured.Group("")
	admin.Use(security.AdminOnly())
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
	mailOps := NewMailOps(db, scheduler.Client, scheduler.Client.PublicSettings())
	admin.POST("/mail/delete", mailOps.MoveToTrash)
	admin.POST("/mail/delete/preview", mailOps.Preview)
	admin.GET("/mail/folders", mailOps.Folders)
	admin.GET("/mail/delete-audit", mailOps.Audit)
	admin.POST("/mail/delete/:id/reconcile", mailOps.Reconcile)
	classificationOps := NewClassificationOps(db)
	admin.PATCH("/classifications/:id", classificationOps.Review)
	admin.POST("/classifications/:id/retry", classificationOps.Requeue)
	admin.GET("/users", security.ListUsers)
	admin.POST("/users", security.CreateUser)
	admin.DELETE("/users/:id", security.DisableUser)
}
