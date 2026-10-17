package auth

import (
	"strings"
	"testing"
	"time"
)

func TestJWTSignVerifyAndTampering(t *testing.T) {
	m, err := New(strings.Repeat("x", 64), "mail-manager", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := m.Sign(17, 2)
	if err != nil {
		t.Fatal(err)
	}
	id, v, err := m.Verify(token)
	if err != nil || id != 17 || v != 2 {
		t.Fatalf("verify id=%d version=%d err=%v", id, v, err)
	}
	tokens := []string{"", token + "bad", "eyJhbGciOiJub25lIn0.e30.abc", strings.Replace(token, ".ey", ".abc", 1)}
	for _, bad := range tokens {
		if _, _, err = m.Verify(bad); err == nil {
			t.Fatalf("accepted invalid JWT")
		}
	}
}
func TestJWTSecretValidation(t *testing.T) {
	if _, err := New("short", "issuer", time.Hour); err == nil {
		t.Fatal("short secret accepted")
	}
}
