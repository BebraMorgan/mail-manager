package mail

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"mail-manager/internal/storage"
)

var ErrScanInProgress = errors.New("mailbox scan already running")

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
		return 0, ErrScanInProgress
	}
	defer s.mu.Unlock()

	return s.Client.Scan(ctx, s.Storage)
}

func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanTicker := time.NewTicker(interval)
		defer scanTicker.Stop()
		scan := func() {
			start := time.Now()
			n, err := s.Scan(ctx)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("IMAP scan failed; will retry", "error", err, "retry_after", interval)
				}
				return
			}
			slog.Info("IMAP scan finished", "registered", n, "duration", time.Since(start))
		}
		scan()
		for {
			select {
			case <-ctx.Done():
				return
			case <-scanTicker.C:
				scan()
			}
		}
	}()
	workerTicker := time.NewTicker(2 * time.Second)
	defer workerTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			select {
			case <-scanDone:
			case <-time.After(5 * time.Second):
				slog.Warn("IMAP scan shutdown timeout")
			}
			return
		case <-workerTicker.C:
			_, err := s.Worker.ProcessOne(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Error("worker failed", "error", err)
			}
		}
	}
}
