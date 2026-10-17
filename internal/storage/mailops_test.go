package storage

import (
	"context"
	"mail-manager/internal/classifier"
	"path/filepath"
	"testing"
	"time"
)

func TestMoveStateAndQueue(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(ctx, []Email{{Account: "a@test.invalid", Folder: "INBOX", UIDValidity: 22, UID: 101, Sender: "b@test.invalid", Subject: "Sample", ReceivedAt: time.Now()}}, "a@test.invalid", "INBOX", 22, 101); err != nil {
		t.Fatal(err)
	}
	if err = s.BootstrapAdmin(ctx, "admin", "some long password 123"); err != nil {
		t.Fatal(err)
	}
	u, err := s.FindUser(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	e, state, err := s.EmailForMailbox(ctx, 1, "a@test.invalid", "INBOX")
	if err != nil || state != "active" || e.UID != 101 {
		t.Fatalf("lookup %v %v", state, err)
	}
	if err = s.BeginMove(ctx, 1, u.ID, "a@test.invalid", "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginMove(ctx, 1, u.ID, "a@test.invalid", "INBOX"); err == nil {
		t.Fatal("double reserve")
	}
	j, err := s.ClaimJob(ctx, time.Minute)
	if err != nil || j != nil {
		t.Fatalf("moving mail claimed: %v %v", j, err)
	}
	if err = s.FinishMove(ctx, 1, u.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	_, state, err = s.EmailForMailbox(ctx, 1, "a@test.invalid", "INBOX")
	if err != nil || state != "deleted" {
		t.Fatalf("after move %v %v", state, err)
	}
}
func TestManualReview(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.BootstrapAdmin(ctx, "admin", "some long password 123"); err != nil {
		t.Fatal(err)
	}
	u, err := s.FindUser(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(ctx, []Email{{Account: "a@test.invalid", Folder: "INBOX", UIDValidity: 22, UID: 101, Sender: "b@test.invalid", Subject: "Sample", ReceivedAt: time.Now()}}, "a@test.invalid", "INBOX", 22, 101); err != nil {
		t.Fatal(err)
	}
	j, err := s.ClaimJob(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	c := Classification{EmailID: 1, SpamVerdict: "not_spam", Importance: "low", Category: "work", ActionType: "none", Summary: "Test", Reason: "Test", Model: "qwen"}
	if err = s.CompleteJob(ctx, j, c); err != nil {
		t.Fatal(err)
	}
	d := ManualClassification{Decision: classifier.Decision{SpamVerdict: "not_spam", Category: "work", Importance: "high", ActionRequired: true, ActionType: "reply", Summary: "Нужен ответ", Reason: "Прямой вопрос"}, IncludeInReport: true}
	if err = s.ReviewClassification(ctx, 1, u.ID, d); err != nil {
		t.Fatal(err)
	}
	item, err := s.GetClassificationItem(ctx, 1)
	if err != nil || item.Importance != "high" || !item.ManualOverride {
		t.Fatalf("review %+v %v", item, err)
	}
}
