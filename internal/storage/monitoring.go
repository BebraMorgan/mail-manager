package storage

import (
	"context"
	"fmt"
	"mail-manager/internal/classifier"
	"strings"
	"time"
)

type QueueStats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Retry      int64 `json:"retry"`
	Skipped    int64 `json:"skipped"`
	Total      int64 `json:"total"`
}

func (s *Storage) QueueStats(ctx context.Context) (QueueStats, error) {
	var stats QueueStats
	rows, err := s.DB.QueryContext(ctx, `SELECT j.status,COUNT(*) FROM classification_jobs j JOIN emails e ON e.id=j.email_id WHERE e.deletion_state='active' GROUP BY j.status`)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		switch status {
		case "pending":
			stats.Pending = count
		case "processing":
			stats.Processing = count
		case "completed":
			stats.Completed = count
		case "retry":
			stats.Retry = count
		case "skipped":
			stats.Skipped = count
		}
		stats.Total += count
	}
	return stats, rows.Err()
}

type ProblemItem struct {
	EmailID       int64  `json:"email_id"`
	Sender        string `json:"sender"`
	Subject       string `json:"subject"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	LastError     string `json:"last_error"`
	Importance    string `json:"importance"`
	NeedsReview   bool   `json:"needs_review"`
	DeletionState string `json:"deletion_state"`
}

func (s *Storage) ListProblems(ctx context.Context, limit int) ([]ProblemItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.sender,e.subject,j.status,j.attempts,j.last_error,COALESCE(c.importance,''),COALESCE(c.needs_review,0),e.deletion_state
 FROM classification_jobs j JOIN emails e ON e.id=j.email_id LEFT JOIN classifications c ON c.email_id=e.id
 WHERE (e.deletion_state='active' AND (j.status IN ('retry','skipped') OR c.needs_review=1 OR c.importance='uncertain')) OR e.deletion_state IN ('moving','unknown')
 ORDER BY j.updated_at DESC,e.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProblemItem, 0)
	for rows.Next() {
		var x ProblemItem
		if err := rows.Scan(&x.EmailID, &x.Sender, &x.Subject, &x.Status, &x.Attempts, &x.LastError, &x.Importance, &x.NeedsReview, &x.DeletionState); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}

type ClassificationItem struct {
	EmailID         int64  `json:"email_id"`
	Sender          string `json:"sender"`
	Subject         string `json:"subject"`
	ReceivedAt      string `json:"received_at"`
	IsSpam          bool   `json:"is_spam"`
	SpamVerdict     string `json:"spam_verdict"`
	Importance      string `json:"importance"`
	Category        string `json:"category"`
	IncludeInReport bool   `json:"include_in_report"`
	ActionRequired  bool   `json:"action_required"`
	ActionType      string `json:"action_type"`
	Summary         string `json:"summary"`
	Reason          string `json:"reason"`
	Model           string `json:"model"`
	BodyTruncated   bool   `json:"body_truncated"`
	NeedsReview     bool   `json:"needs_review"`
	OriginalChars   int    `json:"original_chars"`
	ProcessedChars  int    `json:"processed_chars"`
	ManualOverride  bool   `json:"manual_override"`
	ReviewedAt      string `json:"reviewed_at"`
	DeletionState   string `json:"deletion_state"`
}

const classificationFields = `e.id,e.sender,e.subject,COALESCE(e.received_at,''),c.is_spam,c.spam_verdict,c.importance,c.category,c.include_in_report,c.action_required,c.action_type,c.summary,c.reason,c.model,c.body_truncated,c.needs_review,c.original_chars,c.processed_chars,c.manual_override,COALESCE(c.reviewed_at,''),e.deletion_state`

func scanClassification(rows interface{ Scan(...any) error }) (ClassificationItem, error) {
	var x ClassificationItem
	err := rows.Scan(&x.EmailID, &x.Sender, &x.Subject, &x.ReceivedAt, &x.IsSpam, &x.SpamVerdict, &x.Importance, &x.Category, &x.IncludeInReport, &x.ActionRequired, &x.ActionType, &x.Summary, &x.Reason, &x.Model, &x.BodyTruncated, &x.NeedsReview, &x.OriginalChars, &x.ProcessedChars, &x.ManualOverride, &x.ReviewedAt, &x.DeletionState)
	return x, err
}

type ClassificationFilter struct {
	Limit           int
	Offset          int
	Category        string
	Importance      string
	SpamVerdict     string
	IncludeInReport *bool
	NeedsReview     *bool
	Query           string
}

func (s *Storage) ListClassifications(ctx context.Context, f ClassificationFilter) ([]ClassificationItem, int64, error) {
	if f.Limit < 1 || f.Limit > 200 || f.Offset < 0 || f.Offset > 1000000 {
		return nil, 0, fmt.Errorf("invalid pagination")
	}
	where := []string{"e.deletion_state='active'"}
	args := []any{}
	if f.Category != "" {
		if !contains(classifier.Categories, f.Category) {
			return nil, 0, fmt.Errorf("invalid category")
		}
		where = append(where, "c.category=?")
		args = append(args, f.Category)
	}
	if f.Importance != "" {
		if !contains(classifier.Importances, f.Importance) {
			return nil, 0, fmt.Errorf("invalid importance")
		}
		where = append(where, "c.importance=?")
		args = append(args, f.Importance)
	}
	if f.SpamVerdict != "" {
		if !contains(classifier.SpamVerdicts, f.SpamVerdict) {
			return nil, 0, fmt.Errorf("invalid spam_verdict")
		}
		where = append(where, "c.spam_verdict=?")
		args = append(args, f.SpamVerdict)
	}
	if f.IncludeInReport != nil {
		where = append(where, "c.include_in_report=?")
		args = append(args, *f.IncludeInReport)
	}
	if f.NeedsReview != nil {
		where = append(where, "c.needs_review=?")
		args = append(args, *f.NeedsReview)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		if len([]rune(q)) > 120 {
			return nil, 0, fmt.Errorf("query too long")
		}
		where = append(where, "(e.subject LIKE ? OR e.sender LIKE ? OR c.summary LIKE ?)")
		like := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(q, "\\", "\\\\"), "%", "\\%"), "_", "\\_") + "%"
		where[len(where)-1] = "(e.subject LIKE ? ESCAPE '\\' OR e.sender LIKE ? ESCAPE '\\' OR c.summary LIKE ? ESCAPE '\\')"
		args = append(args, like, like, like)
	}
	from := ` FROM classifications c JOIN emails e ON e.id=c.email_id WHERE ` + strings.Join(where, " AND ")
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+classificationFields+from+` ORDER BY e.received_at DESC,e.id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]ClassificationItem, 0)
	for rows.Next() {
		x, err := scanClassification(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, x)
	}
	return items, total, rows.Err()
}
func contains(vals []string, item string) bool {
	for _, v := range vals {
		if v == item {
			return true
		}
	}
	return false
}
func (s *Storage) GetClassificationItem(ctx context.Context, id int64) (ClassificationItem, error) {
	return scanClassification(s.DB.QueryRowContext(ctx, `SELECT `+classificationFields+` FROM classifications c JOIN emails e ON e.id=c.email_id WHERE e.id=?`, id))
}

type DailyReport struct {
	Date          string               `json:"date"`
	TotalReceived int64                `json:"total_received"`
	Classified    int64                `json:"classified"`
	Spam          int64                `json:"spam"`
	Unprocessed   int64                `json:"unprocessed"`
	NeedsReview   int64                `json:"needs_review"`
	Items         []ClassificationItem `json:"items"`
}

func (s *Storage) GetDailyReport(ctx context.Context, day time.Time) (DailyReport, error) {
	date := day.UTC().Format("2006-01-02")
	r := DailyReport{Date: date, Items: make([]ClassificationItem, 0)}
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(c.email_id),COALESCE(SUM(CASE WHEN c.is_spam=1 THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN c.needs_review=1 THEN 1 ELSE 0 END),0)
 FROM emails e LEFT JOIN classifications c ON c.email_id=e.id WHERE substr(e.received_at,1,10)=? AND e.deletion_state='active'`, date).Scan(&r.TotalReceived, &r.Classified, &r.Spam, &r.NeedsReview)
	if err != nil {
		return r, err
	}
	r.Unprocessed = r.TotalReceived - r.Classified
	rows, err := s.DB.QueryContext(ctx, `SELECT `+classificationFields+` FROM emails e JOIN classifications c ON c.email_id=e.id WHERE substr(e.received_at,1,10)=? AND e.deletion_state='active' AND c.include_in_report=1 ORDER BY CASE c.importance WHEN 'high' THEN 0 WHEN 'uncertain' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,e.received_at DESC LIMIT 1000`, date)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		x, err := scanClassification(rows)
		if err != nil {
			return r, err
		}
		r.Items = append(r.Items, x)
	}
	return r, rows.Err()
}
