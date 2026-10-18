package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSingleAdminAndRefreshRotation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.BootstrapAdmin(ctx, "admin", "very long secret pass"); err != nil {
		t.Fatal(err)
	}
	if err = s.BootstrapAdmin(ctx, "other", "very long secret pass"); err == nil {
		t.Fatal("second account")
	}
	u, err := s.GetUser(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StoreRefresh(ctx, "token1", 1, u.TokenVersion, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RotateRefresh(ctx, "token1", "token2", 1, u.TokenVersion, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RotateRefresh(ctx, "token1", "token3", 1, u.TokenVersion, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("reused token")
	}
	if _, _, err = s.ReadRefresh(ctx, "token2"); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, 1, "very long secret pass", "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ReadRefresh(ctx, "token2"); err == nil {
		t.Fatal("refresh not revoked")
	}
}
