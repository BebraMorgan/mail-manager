package mail

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"unicode/utf8"

	"mail-manager/internal/llm"
	"mail-manager/internal/storage"
)

type Worker struct {
	Client     *Client
	Storage    *storage.Storage
	Classifier *llm.Client
}

func NewWorker(c *Client, s *storage.Storage, l *llm.Client) *Worker {
	return &Worker{Client: c, Storage: s, Classifier: l}
}

func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	// Long enough for IMAP operations and configured local LLM timeout.
	job, err := w.Storage.ClaimJob(ctx, 10*time.Minute)
	if err != nil || job == nil {
		return false, err
	}
	id := job.Email.ID
	heartbeatDone := make(chan struct{})
	heartbeatStop := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-heartbeatStop:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				renewCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := w.Storage.RenewLease(renewCtx, job, 10*time.Minute)
				cancel()
				if err != nil {
					slog.Warn("could not renew job lease", "email_id", id, "error", err)
					return
				}
			}
		}
	}()
	defer func() { close(heartbeatStop); <-heartbeatDone }()
	body, err := w.Client.FetchOne(ctx, job.Email)
	if err != nil {
		if errors.Is(err, ErrMessageGone) {
			return true, w.finish(ctx, job, "skipped", "message_not_found", 0)
		}
		if errors.Is(err, ErrMessageTooLarge) {
			return true, w.finish(ctx, job, "skipped", "message_too_large_needs_review", 0)
		}
		return true, w.retry(ctx, job, "imap_error")
	}
	original := utf8.RuneCountInString(body)
	normalized := NormalizeBody(body)
	if normalized == "" {
		return true, w.finish(ctx, job, "skipped", "no_text_needs_review", 0)
	}
	// Preserve the first part of a message and short quote context. Retry with
	// tighter budgets only when the model cannot fit or finish its JSON.
	var result storage.Classification
	budgets := []int{0, 1500, 850, 450}
	for attempt, capChars := range budgets {
		text := normalized
		if capChars > 0 {
			rs := []rune(normalized)
			if len(rs) > capChars {
				text = string(rs[:capChars])
			}
		}
		result, err = w.Classifier.Classify(ctx, llm.EmailInput{EmailID: id, Sender: job.Email.Sender, Subject: job.Email.Subject, Body: text})
		if err == nil {
			result.OriginalChars = original
			result.BodyTruncated = result.BodyTruncated || len([]rune(text)) < original
			result.NeedsReview = result.NeedsReview || result.BodyTruncated
			if saveErr := w.Storage.CompleteJob(ctx, job, result); saveErr != nil {
				// Return to retry when a DB transaction failed.
				_ = w.finish(context.Background(), job, "retry", "db_write_failed", time.Minute)
				return true, saveErr
			}
			slog.Info("email classified", "email_id", id, "importance", result.Importance, "category", result.Category, "truncated", result.BodyTruncated)
			return true, nil
		}
		if !errors.Is(err, llm.ErrInvalidContext) && !errors.Is(err, llm.ErrInvalidResponse) {
			break
		}
		if attempt == len(budgets)-1 {
			break
		}
	}
	reason := "llm_error"
	if errors.Is(err, llm.ErrInvalidContext) {
		reason = "context_exceeded"
	}
	if errors.Is(err, llm.ErrInvalidResponse) {
		reason = "invalid_llm_json"
	}
	return true, w.retry(ctx, job, reason)
}
func (w *Worker) finish(ctx context.Context, j *storage.Job, status, reason string, delay time.Duration) error {
	// Cancellation during shutdown should not strand the job indefinitely.
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.Storage.FinishJob(writeCtx, j, status, reason, delay)
}
func (w *Worker) retry(ctx context.Context, j *storage.Job, reason string) error {
	tries := j.Attempts + 1
	if tries >= 8 {
		slog.Warn("job requires review", "email_id", j.Email.ID, "reason", reason)
		return w.finish(ctx, j, "skipped", "retry_exhausted_"+reason, 0)
	}
	exponent := tries - 1
	if exponent > 6 {
		exponent = 6
	}
	delay := time.Minute * time.Duration(1<<exponent)
	slog.Warn("job retry scheduled", "email_id", j.Email.ID, "attempts", tries, "reason", reason, "delay", delay)
	return w.finish(ctx, j, "retry", reason, delay)
}
