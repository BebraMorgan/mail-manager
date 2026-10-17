package mail

import (
	"context"
	"fmt"
	"log/slog"

	"mail-manager/internal/llm"
	"mail-manager/internal/storage"
)

type SyncService struct {
	Client     *Client
	Storage    *storage.Storage
	Classifier *llm.Client
}

func NewSyncService(
	client *Client,
	db *storage.Storage,
	classifier *llm.Client,
) *SyncService {
	return &SyncService{
		Client:     client,
		Storage:    db,
		Classifier: classifier,
	}
}

func (s *SyncService) Run(
	ctx context.Context,
) (int, error) {
	processed := 0

	err := s.Client.FetchRecent(
		ctx,
		func(ctx context.Context, msg Message) error {
			id, err := s.Storage.SaveEmail(
				ctx,
				storage.Email{
					Account:     msg.Account,
					Folder:      msg.Folder,
					UIDValidity: msg.UIDValidity,
					UID:         msg.UID,
					Sender:      msg.Sender,
					Subject:     msg.Subject,
					ReceivedAt:  msg.ReceivedAt,
				},
			)
			if err != nil {
				return fmt.Errorf("save email: %w", err)
			}

			existing, err := s.Storage.GetClassification(
				ctx, id,
			)
			if err != nil {
				return err
			}

			if existing != nil {
				return nil
			}

			result, err := s.Classifier.Classify(
				ctx,
				llm.EmailInput{
					EmailID: id,
					Sender:  msg.Sender,
					Subject: msg.Subject,
					Body:    msg.Body,
				},
			)
			if err != nil {
				return fmt.Errorf(
					"classify email %d: %w", id, err,
				)
			}

			if err := s.Storage.SaveClassification(
				ctx, result,
			); err != nil {
				return fmt.Errorf(
					"save classification: %w", err,
				)
			}

			slog.Info(
				"email classified",
				"email_id", id,
				"category", result.Category,
				"importance", result.Importance,
				"spam", result.IsSpam,
				"include_in_report", result.IncludeInReport,
			)

			processed++
			return nil
		},
	)

	return processed, err
}
