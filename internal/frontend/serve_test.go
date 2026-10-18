package frontend

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"index.html":        "<!doctype html><html><body><div id=\"root\"></div></body></html>",
		"assets/app-123.js": "console.log('ready')",
		"favicon.svg":       "<svg></svg>",
		".env":              "DO_NOT_EXPOSE",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := gin.New()
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/api/health/db", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	if err := Register(r, dir); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFrontendServing(t *testing.T) {
	r := testRouter(t)
	cases := []struct {
		method, url           string
		status                int
		includes, contentType string
	}{
		{"GET", "/", 200, "id=\"root\"", "text/html"},
		{"GET", "/login", 200, "id=\"root\"", "text/html"},
		{"GET", "/mail/123", 200, "id=\"root\"", "text/html"},
		{"GET", "/reports?date=2026-10-18", 200, "id=\"root\"", "text/html"},
		{"GET", "/assets/app-123.js", 200, "console.log", "text/javascript"},
		{"GET", "/assets/404.js", 404, "", ""},
		{"GET", "/assets/", 404, "", ""},
		{"GET", "/favicon.svg", 200, "<svg>", "image/svg+xml"},
		{"GET", "/missing.css", 404, "", ""},
		{"GET", "/api/unknown", 404, "route not found", "application/json"},
		{"GET", "/api", 404, "route not found", "application/json"},
		{"GET", "/api/health/db", 200, "ok", "application/json"},
		{"GET", "/health", 200, "ok", "application/json"},
		{"GET", "/.env", 404, "", ""},
		{"GET", "/assets/.private", 404, "", ""},
		{"POST", "/settings", 405, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.url, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.url, nil)
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %q", rec.Code, tc.status, rec.Body.String())
			}
			if tc.includes != "" && !strings.Contains(rec.Body.String(), tc.includes) {
				t.Fatalf("missing %q from %q", tc.includes, rec.Body.String())
			}
			if tc.contentType != "" && !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("Content-Type = %q, need %s", rec.Header().Get("Content-Type"), tc.contentType)
			}
			if tc.status == 404 && strings.Contains(rec.Body.String(), "id=\"root\"") {
				t.Fatalf("unexpected SPA fallback on %s", tc.url)
			}
		})
	}
}

func TestFrontendDistMustExist(t *testing.T) {
	r := gin.New()
	if err := Register(r, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing dist error")
	}
}

func TestSPAIndexDoesNotCache(t *testing.T) {
	r := testRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("index cache header: %q", rec.Header().Get("Cache-Control"))
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-123.js", nil))
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset cache header: %q", rec.Header().Get("Cache-Control"))
	}
}

func TestRejectSymlinkOutsideBuild(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("do-not-expose"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	r := gin.New()
	if err := Register(r, dir); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/leak.txt", nil))
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "do-not-expose") {
		t.Fatalf("symlink leak: status=%d, body=%q", rec.Code, rec.Body.String())
	}
}
