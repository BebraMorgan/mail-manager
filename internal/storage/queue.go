package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Job struct {
	Email      Email
	Attempts   int
	LeaseToken string
}

func (s *Storage) GetCursor(ctx context.Context, account, folder string, validity uint32) (uint32, error) {
	var saved, last uint32
	err := s.DB.QueryRowContext(ctx, `SELECT uid_validity, last_uid FROM mailbox_state WHERE account=? AND folder=?`, account, folder).Scan(&saved, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if saved != validity {
		return 0, nil
	}
	return last, nil
}

// Register a complete FETCH batch and its cursor in one transaction.
func (s *Storage) Enqueue(ctx context.Context, emails []Email, account, folder string, validity, lastUID uint32) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range emails {
		var received any
		if !e.ReceivedAt.IsZero() {
			received = e.ReceivedAt.UTC().Format(time.RFC3339Nano)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO emails(account,folder,uid_validity,uid,sender,subject,received_at,created_at)
            VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(account,folder,uid_validity,uid) DO NOTHING`,
			e.Account, e.Folder, e.UIDValidity, e.UID, e.Sender, e.Subject, received, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("insert email UID %d: %w", e.UID, err)
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO classification_jobs(email_id)
            SELECT e.id FROM emails e LEFT JOIN classifications c ON c.email_id=e.id
            WHERE e.account=? AND e.folder=? AND e.uid_validity=? AND e.uid=? AND c.email_id IS NULL`,
			e.Account, e.Folder, e.UIDValidity, e.UID)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mailbox_state(account,folder,uid_validity,last_uid) VALUES(?,?,?,?)
        ON CONFLICT(account,folder) DO UPDATE SET uid_validity=excluded.uid_validity, last_uid=excluded.last_uid`, account, folder, validity, lastUID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecoverJobs only releases *expired* leases; never steal a live worker's job.
func (s *Storage) RecoverJobs(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE classification_jobs SET status='retry',lease_token='',lease_until=0,
        next_attempt_at=unixepoch(),updated_at=unixepoch() WHERE status='processing' AND lease_until<=unixepoch()`)
	return err
}

// ClaimJob is an atomic UPDATE RETURNING so concurrent workers cannot claim the same row.
func (s *Storage) ClaimJob(ctx context.Context, lease time.Duration) (*Job, error) {
	if lease < time.Minute {
		return nil, fmt.Errorf("lease too short")
	}
	var tokenBytes [24]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	var id int64
	var attempts int
	err := s.DB.QueryRowContext(ctx, `UPDATE classification_jobs
        SET status='processing',lease_token=?,lease_until=unixepoch()+?,updated_at=unixepoch()
        WHERE email_id=(SELECT email_id FROM classification_jobs
            WHERE (status IN ('pending','retry') AND next_attempt_at<=unixepoch() AND EXISTS(SELECT 1 FROM emails e WHERE e.id=classification_jobs.email_id AND e.deletion_state='active'))
               OR (status='processing' AND lease_until<=unixepoch() AND EXISTS(SELECT 1 FROM emails e WHERE e.id=classification_jobs.email_id AND e.deletion_state='active'))
            ORDER BY next_attempt_at,email_id LIMIT 1)
        RETURNING email_id, attempts`, token, int64(lease.Seconds())).Scan(&id, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j := &Job{Attempts: attempts, LeaseToken: token}
	err = s.DB.QueryRowContext(ctx, `SELECT id,account,folder,uid_validity,uid,sender,subject FROM emails WHERE id=?`, id).Scan(
		&j.Email.ID, &j.Email.Account, &j.Email.Folder, &j.Email.UIDValidity, &j.Email.UID, &j.Email.Sender, &j.Email.Subject)
	return j, err
}

// Status update is valid only for the worker holding the active lease.
func (s *Storage) FinishJob(ctx context.Context, j *Job, status, reason string, delay time.Duration) error {
	if len(reason) > 180 {
		reason = reason[:180]
	}
	next := time.Now().Add(delay).Unix()
	res, err := s.DB.ExecContext(ctx, `UPDATE classification_jobs
      SET status=?, attempts=attempts+1,last_error=?,next_attempt_at=?,lease_token='',lease_until=0,updated_at=unixepoch()
      WHERE email_id=? AND status='processing' AND lease_token=?`, status, reason, next, j.Email.ID, j.LeaseToken)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("job lease no longer owned")
	}
	return nil
}

// Atomic classification and completion; no half-committed results.
func (s *Storage) CompleteJob(ctx context.Context, j *Job, c Classification) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE classification_jobs SET status='completed',last_error='',lease_token='',lease_until=0,
        updated_at=unixepoch() WHERE email_id=? AND status='processing' AND lease_token=? AND EXISTS(SELECT 1 FROM emails e WHERE e.id=classification_jobs.email_id AND e.deletion_state='active')`, c.EmailID, j.LeaseToken)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("job lease no longer owned")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO classifications(email_id,is_spam,importance,include_in_report,category,reason,model,classified_at,
        body_truncated,original_chars,processed_chars,needs_review,spam_verdict,action_required,action_type,summary) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(email_id) DO UPDATE SET is_spam=excluded.is_spam,importance=excluded.importance,
        include_in_report=excluded.include_in_report,category=excluded.category,reason=excluded.reason,model=excluded.model,
        classified_at=excluded.classified_at,body_truncated=excluded.body_truncated,
        original_chars=excluded.original_chars,processed_chars=excluded.processed_chars,needs_review=excluded.needs_review,
        spam_verdict=excluded.spam_verdict,action_required=excluded.action_required,action_type=excluded.action_type,summary=excluded.summary`,
		c.EmailID, c.IsSpam, c.Importance, c.IncludeInReport, c.Category, c.Reason, c.Model, time.Now().UTC().Format(time.RFC3339Nano),
		c.BodyTruncated, c.OriginalChars, c.ProcessedChars, c.NeedsReview, c.SpamVerdict, c.ActionRequired, c.ActionType, c.Summary)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RenewLease keeps long-running classification jobs from being stolen.
func (s *Storage) RenewLease(ctx context.Context, j *Job, lease time.Duration) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE classification_jobs SET lease_until=unixepoch()+?,updated_at=unixepoch()
        WHERE email_id=? AND status='processing' AND lease_token=?`, int64(lease.Seconds()), j.Email.ID, j.LeaseToken)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("lease no longer owned")
	}
	return nil
}
