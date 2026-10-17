package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Email struct {
	ID          int64
	Account     string
	Folder      string
	UIDValidity uint32
	UID         uint32
	Sender      string
	Subject     string
	ReceivedAt  time.Time
}

type Classification struct {
	EmailID         int64
	IsSpam          bool
	Importance      string
	IncludeInReport bool
	Category        string
	Reason          string
	Model           string
}

type ReportItem struct {
	EmailID    int64  `json:"email_id"`
	Sender     string `json:"sender"`
	Subject    string `json:"subject"`
	Importance string `json:"importance"`
	Category   string `json:"category"`
	Reason     string `json:"reason"`
}

func (s *Storage) SaveEmail(
	ctx context.Context,
	email Email,
) (int64, error) {
	if email.Account == "" || email.Folder == "" ||
		email.UID == 0 || email.UIDValidity == 0 {
		return 0, fmt.Errorf("invalid IMAP email identity")
	}

	var receivedAt any
	if !email.ReceivedAt.IsZero() {
		receivedAt = email.ReceivedAt.UTC().Format(time.RFC3339Nano)
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO emails (
			account, folder, uid_validity, uid,
			sender, subject, received_at, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account, folder, uid_validity, uid)
		DO NOTHING
	`,
		email.Account,
		email.Folder,
		email.UIDValidity,
		email.UID,
		email.Sender,
		email.Subject,
		receivedAt,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("save email: %w", err)
	}

	var id int64
	err = s.DB.QueryRowContext(ctx, `
		SELECT id FROM emails
		WHERE account = ?
		  AND folder = ?
		  AND uid_validity = ?
		  AND uid = ?
	`,
		email.Account,
		email.Folder,
		email.UIDValidity,
		email.UID,
	).Scan(&id)

	return id, err
}

func (s *Storage) GetClassification(
	ctx context.Context,
	emailID int64,
) (*Classification, error) {
	var c Classification

	err := s.DB.QueryRowContext(ctx, `
		SELECT email_id, is_spam, importance,
		       include_in_report, category, reason, model
		FROM classifications
		WHERE email_id = ?
	`, emailID).Scan(
		&c.EmailID,
		&c.IsSpam,
		&c.Importance,
		&c.IncludeInReport,
		&c.Category,
		&c.Reason,
		&c.Model,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &c, nil
}

func (s *Storage) SaveClassification(
	ctx context.Context,
	c Classification,
) error {
	switch c.Importance {
	case "low", "medium", "high", "uncertain":
	default:
		return fmt.Errorf("invalid importance: %q", c.Importance)
	}

	_, err := s.DB.ExecContext(ctx, `
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
		c.EmailID,
		c.IsSpam,
		c.Importance,
		c.IncludeInReport,
		c.Category,
		c.Reason,
		c.Model,
		time.Now().UTC().Format(time.RFC3339Nano),
	)

	return err
}

func (s *Storage) GetReportItems(
	ctx context.Context,
) ([]ReportItem, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			e.id,
			e.sender,
			e.subject,
			c.importance,
			c.category,
			c.reason
		FROM emails e
		JOIN classifications c ON c.email_id = e.id
		WHERE c.include_in_report = 1
		ORDER BY e.received_at DESC, e.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ReportItem, 0)

	for rows.Next() {
		var item ReportItem

		if err := rows.Scan(
			&item.EmailID,
			&item.Sender,
			&item.Subject,
			&item.Importance,
			&item.Category,
			&item.Reason,
		); err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	return items, rows.Err()
}
