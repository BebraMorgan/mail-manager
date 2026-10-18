package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationClaimAndCompletion(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal("migrate twice:", err)
	}
	e := Email{Account: "mail@example.test", Folder: "INBOX", UIDValidity: 12, UID: 7, Sender: "someone@example.test", Subject: "test", ReceivedAt: time.Now().UTC()}
	if err = s.Enqueue(ctx, []Email{e}, e.Account, e.Folder, e.UIDValidity, e.UID); err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(ctx, []Email{e}, e.Account, e.Folder, e.UIDValidity, e.UID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM emails").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate email rows: %d %v", count, err)
	}
	j, err := s.ClaimJob(ctx, 10*time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim: %v %v", j, err)
	}
	again, err := s.ClaimJob(ctx, 10*time.Minute)
	if err != nil || again != nil {
		t.Fatalf("duplicate claim: %v %v", again, err)
	}
	if err = s.CompleteJob(ctx, j, Classification{EmailID: j.Email.ID, Importance: "high", Category: "work", Model: "test", Reason: "requires action", IncludeInReport: true, SpamVerdict: "not_spam", ActionType: "none", Summary: "Test classification"}); err != nil {
		t.Fatal(err)
	}
	_, err = s.ClaimJob(ctx, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stats := QueueStats{}
	stats, err = s.QueueStats(ctx)
	if err != nil || stats.Completed != 1 {
		t.Fatalf("queue stats %v %v", stats, err)
	}
}

func TestBootstrapUserAndNoDuplicate(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.BootstrapAdmin(ctx, "admin", "a long password 123"); err != nil {
		t.Fatal(err)
	}
	if err = s.BootstrapAdmin(ctx, "other", "a long password 123"); err == nil {
		t.Fatal("second bootstrap succeeded")
	}
	u, err := s.FindUser(ctx, "ADMIN")
	if err != nil || u.ID != 1 {
		t.Fatalf("find admin: %v %v", u, err)
	}
	if err = s.ChangePassword(ctx, u.ID, "a long password 123", "another long password 456"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetUser(ctx, u.ID)
	if err != nil || updated.TokenVersion <= u.TokenVersion {
		t.Fatalf("version not rotated: %v", err)
	}
}
