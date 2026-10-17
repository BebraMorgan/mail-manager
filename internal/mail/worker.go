package mail

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"mail-manager/internal/llm"
	"mail-manager/internal/storage"
)

type Worker struct {
	Client     *Client
	Storage    *storage.Storage
	Classifier *llm.Client
}

func NewWorker(
	client *Client,
	db *storage.Storage,
	classifier *llm.Client,
) *Worker {
	return &Worker{
		Client:     client,
		Storage:    db,
		Classifier: classifier,
	}
}

func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	job, err := w.Storage.NextJob(ctx)
	if err != nil || job == nil {
		return false, err
	}

	id := job.Email.ID

	if err := w.Storage.SetJobStatus(
		ctx, id, "processing", "", job.Attempts, 0,
	); err != nil {
		return false, err
	}

	body, err := w.Client.FetchOne(ctx, job.Email)
	if err != nil {
		if errors.Is(err, ErrMessageGone) ||
			errors.Is(err, ErrMessageTooLarge) {
			reason := "message unavailable"
			if errors.Is(err, ErrMessageTooLarge) {
				reason = "message too large"
			}

			return true, w.Storage.SetJobStatus(
				ctx, id, "skipped",
				reason, job.Attempts+1, 0,
			)
		}

		return true, w.retry(ctx, id, job.Attempts, err)
	}

	result, err := w.Classifier.Classify(
		ctx,
		llm.EmailInput{
			EmailID: id,
			Sender:  job.Email.Sender,
			Subject: job.Email.Subject,
			Body:    body,
		},
	)
	if err != nil {
		return true, w.retry(ctx, id, job.Attempts, err)
	}

	if err := w.Storage.CompleteJob(ctx, result); err != nil {
		return true, err
	}

	slog.Info(
		"email classified",
		"email_id", id,
		"category", result.Category,
		"importance", result.Importance,
		"report", result.IncludeInReport,
	)

	return true, nil
}

func (w *Worker) retry(
	ctx context.Context,
	id int64,
	attempts int,
	cause error,
) error {
	attempts++

	delay := time.Minute * time.Duration(1<<min(attempts-1, 6))
	if delay > time.Hour {
		delay = time.Hour
	}

	slog.Warn(
		"classification failed",
		"email_id", id,
		"attempts", attempts,
		"retry_in", delay,
		"error", cause,
	)

	return w.Storage.SetJobStatus(
		ctx, id, "retry",
		"classification or IMAP request failed",
		attempts, delay,
	)
}
