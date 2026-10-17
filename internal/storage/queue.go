package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Job struct {
	Email    Email
	Attempts int
}

func (s *Storage) GetCursor(
	ctx context.Context,
	account, folder string,
	validity uint32,
) (uint32, error) {
	var savedValidity, lastUID uint32

	err := s.DB.QueryRowContext(ctx, `
		SELECT uid_validity, last_uid
		FROM mailbox_state
		WHERE account = ? AND folder = ?
	`, account, folder).Scan(
		&savedValidity, &lastUID,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	if savedValidity != validity {
		return 0, nil
	}
	return lastUID, nil
}

func (s *Storage) RecoverJobs(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE classification_jobs
		SET status = 'retry',
		    next_attempt_at = unixepoch(),
		    updated_at = unixepoch()
		WHERE status = 'processing'
	`)
	return err
}

func (s *Storage) Enqueue(
	ctx context.Context,
	emails []Email,
	account, folder string,
	validity, lastUID uint32,
) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, e := range emails {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO emails (
				account, folder, uid_validity, uid,
				sender, subject, received_at, created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account, folder, uid_validity, uid)
			DO NOTHING
		`,
			e.Account, e.Folder,
			e.UIDValidity, e.UID,
			e.Sender, e.Subject,
			e.ReceivedAt.UTC().Format(time.RFC3339Nano),
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO classification_jobs(email_id)
			SELECT e.id FROM emails e
			LEFT JOIN classifications c ON c.email_id = e.id
			WHERE e.account = ?
			  AND e.folder = ?
			  AND e.uid_validity = ?
			  AND e.uid = ?
			  AND c.email_id IS NULL
		`, e.Account, e.Folder, e.UIDValidity, e.UID)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO mailbox_state(
			account, folder, uid_validity, last_uid
		)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(account, folder) DO UPDATE SET
			uid_validity = excluded.uid_validity,
			last_uid = excluded.last_uid
	`, account, folder, validity, lastUID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Storage) NextJob(ctx context.Context) (*Job, error) {
	var j Job

	err := s.DB.QueryRowContext(ctx, `
		SELECT e.id, e.account, e.folder,
		       e.uid_validity, e.uid,
		       e.sender, e.subject, j.attempts
		FROM classification_jobs j
		JOIN emails e ON e.id = j.email_id
		WHERE j.status IN ('pending', 'retry')
		  AND j.next_attempt_at <= unixepoch()
		ORDER BY j.next_attempt_at, e.id
		LIMIT 1
	`).Scan(
		&j.Email.ID,
		&j.Email.Account,
		&j.Email.Folder,
		&j.Email.UIDValidity,
		&j.Email.UID,
		&j.Email.Sender,
		&j.Email.Subject,
		&j.Attempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Storage) SetJobStatus(
	ctx context.Context,
	id int64,
	status, reason string,
	attempts int,
	delay time.Duration,
) error {
	next := time.Now().Add(delay).Unix()

	_, err := s.DB.ExecContext(ctx, `
		UPDATE classification_jobs
		SET status = ?, attempts = ?,
		    last_error = ?,
		    next_attempt_at = ?,
		    updated_at = unixepoch()
		WHERE email_id = ?
	`, status, attempts, reason, next, id)

	return err
}

func (s *Storage) CompleteJob(
	ctx context.Context,
	c Classification,
) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO classifications (
			email_id, is_spam, importance,
			include_in_report, category, reason,
			model, classified_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(email_id) DO UPDATE SET
			is_spam = excluded.is_spam,
			importance = excluded.importance,
			include_in_report = excluded.include_in_report,
			category = excluded.category,
			reason = excluded.reason,
			model = excluded.model,
			classified_at = excluded.classified_at
	`,
		c.EmailID, c.IsSpam, c.Importance,
		c.IncludeInReport, c.Category, c.Reason,
		c.Model, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save classification: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE classification_jobs
		SET status = 'completed',
		    last_error = '',
		    updated_at = unixepoch()
		WHERE email_id = ?
	`, c.EmailID)
	if err != nil {
		return err
	}

	return tx.Commit()
}
