package mail

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"mail-manager/internal/storage"
)

type Scheduler struct {
	Client  *Client
	Storage *storage.Storage
	Worker  *Worker

	mu sync.Mutex
}

func NewScheduler(
	client *Client,
	db *storage.Storage,
	worker *Worker,
) *Scheduler {
	return &Scheduler{
		Client:  client,
		Storage: db,
		Worker:  worker,
	}
}

func (s *Scheduler) Scan(
	ctx context.Context,
) (int, error) {
	// Prevent two scans in the same process.
	if !s.mu.TryLock() {
		return 0, nil
	}
	defer s.mu.Unlock()

	return s.Client.Scan(ctx, s.Storage)
}

func (s *Scheduler) Run(
	ctx context.Context,
	interval time.Duration,
) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	scanTicker := time.NewTicker(interval)
	workerTicker := time.NewTicker(2 * time.Second)

	defer scanTicker.Stop()
	defer workerTicker.Stop()

	scan := func() {
		if ctx.Err() != nil {
			return
		}

		start := time.Now()

		count, err := s.Scan(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn(
					"IMAP scan failed; will retry",
					"error", err,
					"retry_after", interval,
				)
			}
			return
		}

		slog.Info(
			"IMAP scan completed",
			"registered", count,
			"duration", time.Since(start),
		)
	}

	// Initial sync.
	scan()

	for {
		select {
		case <-ctx.Done():
			slog.Info("mail scheduler stopped")
			return

		case <-scanTicker.C:
			scan()

		case <-workerTicker.C:
			if ctx.Err() != nil {
				return
			}

			_, err := s.Worker.ProcessOne(ctx)

			if err != nil && ctx.Err() == nil {
				slog.Error(
					"classification worker failed",
					"error", err,
				)
			}
		}
	}
}
