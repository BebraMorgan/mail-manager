package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"mail-manager/internal/classifier"
	"mail-manager/internal/mail"
	"mail-manager/internal/storage"
)

type MailOps struct {
	db   *storage.Storage
	imap *mail.Client
	cfg  mail.PublicSettings
}

func NewMailOps(db *storage.Storage, imap *mail.Client, cfg mail.PublicSettings) *MailOps {
	return &MailOps{db: db, imap: imap, cfg: cfg}
}

// Human-only: must be registered inside the authenticated admin group.
func (h *MailOps) MoveToTrash(c *gin.Context) {
	if !h.cfg.AllowDelete {
		c.JSON(http.StatusForbidden, gin.H{"error": "mail deletion disabled in configuration"})
		return
	}
	var req struct {
		IDs      []int64 `json:"ids"`
		Confirm  string  `json:"confirm"`
		Password string  `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Confirm != "MOVE_TO_TRASH" || len(req.IDs) == 0 || len(req.IDs) > 100 {
		c.JSON(400, gin.H{"error": "provide 1..100 email IDs and confirm=MOVE_TO_TRASH"})
		return
	}
	actor := CurrentUser(c)
	if actor == nil {
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}
	// An access token alone is insufficient to change the mailbox.
	// The React UI must request the administrator's password at confirmation.
	if req.Password == "" || len(req.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(actor.PasswordHash), []byte(req.Password)) != nil {
		c.JSON(403, gin.H{"error": "confirmation password invalid"})
		return
	}
	// Deduplicate to protect against two moves of the same ID within the request.
	seen := make(map[int64]bool, len(req.IDs))
	out := make([]gin.H, 0, len(req.IDs))
	bad := false
	for _, id := range req.IDs {
		if id <= 0 {
			bad = true
			out = append(out, gin.H{"id": id, "status": "invalid_id"})
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ctx, cancel := context.WithTimeout(c.Request.Context(), 55*time.Second)
		email, state, err := h.db.EmailForMailbox(ctx, id, h.cfg.Username, h.cfg.Folder)
		if errors.Is(err, storage.ErrMissingEmail) {
			bad = true
			out = append(out, gin.H{"id": id, "status": "not_found"})
			cancel()
			continue
		}
		if err != nil {
			bad = true
			out = append(out, gin.H{"id": id, "status": "database_error"})
			cancel()
			continue
		}
		if state == "deleted" {
			out = append(out, gin.H{"id": id, "status": "already_deleted"})
			cancel()
			continue
		}
		if state != "active" {
			bad = true
			out = append(out, gin.H{"id": id, "status": "needs_reconciliation"})
			cancel()
			continue
		}
		if err = h.db.BeginMove(ctx, id, actor.ID, h.cfg.Username, h.cfg.Folder); err != nil {
			bad = true
			out = append(out, gin.H{"id": id, "status": "conflict"})
			cancel()
			continue
		}
		err = h.imap.MoveToTrash(ctx, email)
		if err != nil {
			if errors.Is(err, mail.ErrMoveUnsupported) || errors.Is(err, mail.ErrUIDValidityChanged) || errors.Is(err, mail.ErrMessageGone) {
				reason := "pre_move_check_failed"
				if errors.Is(err, mail.ErrMoveUnsupported) {
					reason = "imap_move_unsupported"
				}
				if errors.Is(err, mail.ErrUIDValidityChanged) {
					reason = "uidvalidity_changed"
				}
				if errors.Is(err, mail.ErrMessageGone) {
					reason = "uid_not_found"
				}
				if cancelErr := h.db.CancelMove(context.Background(), id, actor.ID, reason); cancelErr != nil {
					slog.Error("could not revert pre-MOVE reservation", "email_id", id, "error", cancelErr)
					bad = true
					out = append(out, gin.H{"id": id, "status": "needs_reconciliation"})
				} else {
					bad = true
					out = append(out, gin.H{"id": id, "status": reason})
				}
				cancel()
				continue
			}
			slog.Warn("manual IMAP MOVE unconfirmed", "email_id", id, "error", err)
			// On network errors MOVE may have succeeded despite an error response.
			// Do not automatically retry; a human must reconcile mailbox state.
			storeErr := h.db.FinishMove(context.Background(), id, actor.ID, false, "imap_move_unconfirmed")
			if storeErr != nil {
				slog.Error("failed to record move outcome", "email_id", id, "error", storeErr)
			}
			bad = true
			out = append(out, gin.H{"id": id, "status": "needs_reconciliation"})
			cancel()
			continue
		}
		if err = h.db.FinishMove(ctx, id, actor.ID, true, ""); err != nil {
			slog.Error("IMAP MOVE succeeded but database commit failed", "email_id", id, "error", err)
			bad = true
			out = append(out, gin.H{"id": id, "status": "moved_on_server_db_error"})
			cancel()
			continue
		}
		out = append(out, gin.H{"id": id, "status": "moved_to_trash"})
		cancel()
	}
	status := http.StatusOK
	if bad {
		status = http.StatusMultiStatus
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"items": out, "count": len(out)})
}
func (h *MailOps) Content(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	email, state, err := h.db.EmailForMailbox(c.Request.Context(), id, h.cfg.Username, h.cfg.Folder)
	if err != nil || state != "active" {
		c.JSON(404, gin.H{"error": "mail not available"})
		return
	}
	// BODY.PEEK, read-only EXAMINE.  Mail body is never persisted to SQLite.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	body, err := h.imap.FetchOne(ctx, email)
	if errors.Is(err, mail.ErrMessageTooLarge) {
		c.JSON(413, gin.H{"error": "message exceeds configured size limit"})
		return
	}
	if err != nil {
		c.JSON(502, gin.H{"error": "unable to read IMAP message"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"id": id, "sender": email.Sender, "subject": email.Subject, "body": body})
}
func (h *MailOps) Audit(c *gin.Context) {
	items, err := h.db.DeleteAudit(c.Request.Context(), 50)
	if err != nil {
		c.JSON(500, gin.H{"error": "database error"})
		return
	}
	c.JSON(200, gin.H{"items": items})
}

type ClassificationOps struct{ db *storage.Storage }

func NewClassificationOps(db *storage.Storage) *ClassificationOps { return &ClassificationOps{db: db} }
func (h *ClassificationOps) Review(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var request struct {
		classifier.Decision
		IncludeInReport *bool `json:"include_in_report"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.IncludeInReport == nil {
		c.JSON(400, gin.H{"error": "invalid classification review"})
		return
	}
	decision := request.Decision
	if err := classifier.Validate(decision); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	user := CurrentUser(c)
	err := h.db.ReviewClassification(c.Request.Context(), id, user.ID, storage.ManualClassification{Decision: decision, IncludeInReport: *request.IncludeInReport})
	if err != nil {
		c.JSON(409, gin.H{"error": "classification is not available for review"})
		return
	}
	c.JSON(200, gin.H{"status": "reviewed", "email_id": id})
}
func (h *ClassificationOps) Requeue(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.db.RequeueClassification(c.Request.Context(), id); err != nil {
		c.JSON(409, gin.H{"error": "email cannot be requeued"})
		return
	}
	c.JSON(202, gin.H{"status": "queued", "email_id": id})
}

func (h *MailOps) Folders(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()
	folders, err := h.imap.ListFolders(ctx)
	if err != nil {
		c.JSON(502, gin.H{"error": "unable to list IMAP folders"})
		return
	}
	c.JSON(200, gin.H{"items": folders})
}

// Preview is read-only. React should display the subjects before asking for
// a separate explicit confirmation of POST /api/mail/delete.
func (h *MailOps) Preview(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 100 {
		c.JSON(400, gin.H{"error": "provide 1..100 ids"})
		return
	}
	out := make([]gin.H, 0, len(req.IDs))
	seen := make(map[int64]bool)
	for _, id := range req.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if id <= 0 {
			out = append(out, gin.H{"id": id, "status": "invalid_id"})
			continue
		}
		e, state, err := h.db.EmailForMailbox(c.Request.Context(), id, h.cfg.Username, h.cfg.Folder)
		if err != nil {
			out = append(out, gin.H{"id": id, "status": "not_found"})
			continue
		}
		out = append(out, gin.H{"id": id, "sender": e.Sender, "subject": e.Subject, "status": state})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"items": out, "enabled": h.cfg.AllowDelete, "action": "move_to_trash", "destination_configured": h.cfg.TrashFolder != ""})
}

func (h *MailOps) Reconcile(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		Resolution string `json:"resolution"`
		Password   string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Resolution != "in_inbox" && req.Resolution != "moved_to_trash") {
		c.JSON(400, gin.H{"error": "invalid resolution"})
		return
	}
	user := CurrentUser(c)
	if req.Password == "" || len(req.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		c.JSON(403, gin.H{"error": "confirmation password invalid"})
		return
	}
	// Restrict reconciliation to this configured mailbox.
	if _, _, err := h.db.EmailForMailbox(c.Request.Context(), id, h.cfg.Username, h.cfg.Folder); err != nil {
		c.JSON(404, gin.H{"error": "mail not found"})
		return
	}
	if err := h.db.ReconcileMove(c.Request.Context(), id, user.ID, req.Resolution); err != nil {
		c.JSON(409, gin.H{"error": "mail not awaiting reconciliation"})
		return
	}
	c.JSON(200, gin.H{"id": id, "status": "reconciled", "resolution": req.Resolution})
}
