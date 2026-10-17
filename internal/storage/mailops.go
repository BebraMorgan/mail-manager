package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mail-manager/internal/classifier"
	"time"
)

var ErrMissingEmail = errors.New("email not found")
var ErrEmailConflict = errors.New("email unavailable or operation already in progress")

// EmailForMailbox ensures API IDs can only affect the explicitly configured mailbox.
func (s *Storage) EmailForMailbox(ctx context.Context, id int64, account, folder string) (Email, string, error) {
	var e Email
	var state string
	err := s.DB.QueryRowContext(ctx, `SELECT id,account,folder,uid_validity,uid,sender,subject,deletion_state FROM emails WHERE id=? AND account=? AND folder=?`, id, account, folder).Scan(&e.ID, &e.Account, &e.Folder, &e.UIDValidity, &e.UID, &e.Sender, &e.Subject, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return e, "", ErrMissingEmail
	}
	return e, state, err
}

// BeginMove reserves the email and records which human requested the action.
func (s *Storage) BeginMove(ctx context.Context, id, actor int64, account, folder string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE emails SET deletion_state='moving' WHERE id=? AND account=? AND folder=? AND deletion_state='active'`, id, account, folder)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrEmailConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_delete_audit(email_id,actor_user_id,status,created_at) VALUES(?,?, 'started',?)`, id, actor, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// FinishMove records a successful IMAP MOVE or an ambiguous failure.
func (s *Storage) FinishMove(ctx context.Context, id, actor int64, ok bool, errorCode string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if ok {
		_, err = tx.ExecContext(ctx, `UPDATE emails SET deletion_state='deleted',deleted_at=?,deleted_by=? WHERE id=? AND deletion_state IN ('moving','unknown')`, time.Now().UTC().Format(time.RFC3339Nano), actor, id)
		if err != nil {
			return err
		}
		// A deleted message must not remain in the processing queue or the report.
		_, err = tx.ExecContext(ctx, `UPDATE classification_jobs SET status='skipped',lease_token='',lease_until=0,last_error='human_moved_to_trash',updated_at=unixepoch() WHERE email_id=?`, id)
		if err != nil {
			return err
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE emails SET deletion_state='unknown' WHERE id=? AND deletion_state='moving'`, id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE classification_jobs SET status='skipped',lease_token='',lease_until=0,last_error='mail_move_uncertain_needs_review',updated_at=unixepoch() WHERE email_id=?`, id)
		if err != nil {
			return err
		}
	}
	status := "uncertain"
	if ok {
		status = "success"
	}
	if len(errorCode) > 120 {
		errorCode = errorCode[:120]
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_delete_audit(email_id,actor_user_id,status,error_code,created_at) VALUES(?,?,?,?,?)`, id, actor, status, errorCode, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

type ManualClassification struct {
	classifier.Decision
	IncludeInReport bool `json:"include_in_report"`
}

func (s *Storage) ReviewClassification(ctx context.Context, id, user int64, d ManualClassification) error {
	if err := classifier.Validate(d.Decision); err != nil {
		return err
	}
	// No manual modification of deleted mail or running jobs.
	res, err := s.DB.ExecContext(ctx, `UPDATE classifications SET spam_verdict=?,is_spam=?,importance=?,category=?,action_required=?,action_type=?,summary=?,reason=?,include_in_report=?,needs_review=0,manual_override=1,reviewed_by=?,reviewed_at=?
 WHERE email_id=? AND EXISTS (SELECT 1 FROM emails e WHERE e.id=? AND e.deletion_state='active')
 AND EXISTS (SELECT 1 FROM classification_jobs j WHERE j.email_id=? AND j.status='completed')`,
		d.SpamVerdict, d.SpamVerdict == "spam", d.Importance, d.Category, d.ActionRequired, d.ActionType, d.Summary, d.Reason, d.IncludeInReport, user, time.Now().UTC().Format(time.RFC3339Nano), id, id, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrEmailConflict
	}
	return nil
}

// Explicit administrator request can requeue even completed/skipped messages.
// The old classification is retained until the new one is committed.
func (s *Storage) RequeueClassification(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE classification_jobs SET status='pending',attempts=0,next_attempt_at=0,last_error='',lease_until=0,lease_token='',updated_at=unixepoch()
 WHERE email_id=? AND status!='processing' AND EXISTS(SELECT 1 FROM emails e WHERE e.id=? AND e.deletion_state='active')`, id, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrEmailConflict
	}
	// Retries explicitly clear the human override. Classification remains readable meanwhile.
	_, err = tx.ExecContext(ctx, `UPDATE classifications SET manual_override=0, reviewed_by=NULL,reviewed_at=NULL WHERE email_id=?`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Storage) GetEmailContentMetadata(ctx context.Context, id int64) (Email, string, error) {
	var e Email
	var state string
	err := s.DB.QueryRowContext(ctx, `SELECT id,account,folder,uid_validity,uid,sender,subject,deletion_state FROM emails WHERE id=?`, id).Scan(&e.ID, &e.Account, &e.Folder, &e.UIDValidity, &e.UID, &e.Sender, &e.Subject, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return e, "", ErrMissingEmail
	}
	return e, state, err
}

func (s *Storage) DeleteAudit(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid limit")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,email_id,actor_user_id,status,error_code,created_at FROM mail_delete_audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, email, actor int64
		var status, reason, created string
		if err := rows.Scan(&id, &email, &actor, &status, &reason, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "email_id": email, "actor_id": actor, "status": status, "error_code": reason, "created_at": created})
	}
	return out, rows.Err()
}

// CancelMove handles failures that are certain to have happened before any MOVE.
func (s *Storage) CancelMove(ctx context.Context, id, actor int64, code string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE emails SET deletion_state='active' WHERE id=? AND deletion_state='moving'`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrEmailConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_delete_audit(email_id,actor_user_id,status,error_code,created_at) VALUES(?,?,'failed',?,?)`, id, actor, code, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecoverMoveReservations marks interrupted moves as uncertain on server start.
// It never issues IMAP commands. A human must resolve the remote outcome.
func (s *Storage) RecoverMoveReservations(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE emails SET deletion_state='unknown' WHERE deletion_state='moving'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReconcileMove must only be called after a human has checked Yandex Mail.
func (s *Storage) ReconcileMove(ctx context.Context, id, actor int64, resolution string) error {
	if resolution != "in_inbox" && resolution != "moved_to_trash" {
		return fmt.Errorf("invalid resolution")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	newState := "active"
	if resolution == "moved_to_trash" {
		newState = "deleted"
	}
	res, err := tx.ExecContext(ctx, `UPDATE emails SET deletion_state=?,deleted_at=CASE WHEN ?='deleted' THEN ? ELSE NULL END,deleted_by=CASE WHEN ?='deleted' THEN ? ELSE NULL END WHERE id=? AND deletion_state='unknown'`, newState, newState, time.Now().UTC().Format(time.RFC3339Nano), newState, actor, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrEmailConflict
	}
	if newState == "active" {
		_, err = tx.ExecContext(ctx, `UPDATE classification_jobs SET status=CASE WHEN EXISTS(SELECT 1 FROM classifications c WHERE c.email_id=classification_jobs.email_id) THEN 'completed' ELSE 'retry' END, next_attempt_at=unixepoch(),last_error='',lease_token='',lease_until=0,updated_at=unixepoch() WHERE email_id=?`, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE classification_jobs SET status='skipped',lease_token='',lease_until=0,last_error='human_confirmed_in_trash',updated_at=unixepoch() WHERE email_id=?`, id)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_delete_audit(email_id,actor_user_id,action,status,error_code,created_at) VALUES(?,?,'manual_reconciliation','success',?,?)`, id, actor, resolution, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}
