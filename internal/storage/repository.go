package storage

import (
	"context"
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
	BodyTruncated   bool
	OriginalChars   int
	ProcessedChars  int
	NeedsReview     bool
	SpamVerdict     string
	ActionRequired  bool
	ActionType      string
	Summary         string
}

type ReportItem struct {
	EmailID    int64  `json:"email_id"`
	Sender     string `json:"sender"`
	Subject    string `json:"subject"`
	Importance string `json:"importance"`
	Category   string `json:"category"`
	Reason     string `json:"reason"`
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
		WHERE c.include_in_report = 1 AND e.deletion_state = 'active'
		ORDER BY e.received_at DESC, e.id DESC
        LIMIT 1000
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
