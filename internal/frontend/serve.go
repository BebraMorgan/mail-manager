// Package frontend serves the production Vite build from the same Gin origin as the API.
// Static resources are never replaced by index.html when they are missing.
package frontend

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Register installs a fallback for React Router paths. Register all API routes
// before calling Register. distDir must point at a completed Vite build.
func Register(router *gin.Engine, distDir string) error {
	if distDir == "" {
		return fmt.Errorf("frontend dist directory is empty")
	}
	absolute, err := filepath.Abs(distDir)
	if err != nil {
		return fmt.Errorf("resolve frontend dist: %w", err)
	}
	indexPath := filepath.Join(absolute, "index.html")
	info, err := os.Stat(indexPath)
	if err != nil {
		return fmt.Errorf("frontend index.html not found at %q: %w", indexPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("frontend index.html is not a regular file: %q", indexPath)
	}

	publicFS := os.DirFS(absolute)
	fileServer := http.FileServer(http.Dir(absolute))

	router.NoRoute(func(c *gin.Context) {
		urlPath := c.Request.URL.Path

		// Never return HTML for unknown API routes: Axios expects a real 404.
		if urlPath == "/api" || strings.HasPrefix(urlPath, "/api/") ||
			urlPath == "/health" || strings.HasPrefix(urlPath, "/health/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusMethodNotAllowed)
			return
		}

		// Never expose hidden files, dot-segments or directory listings.
		// The dist directory should contain only public Vite build output.
		if !safeURLPath(urlPath) {
			c.Status(http.StatusNotFound)
			return
		}
		requested := strings.TrimPrefix(path.Clean(urlPath), "/")
		if requested == "." || requested == "" || requested == "index.html" {
			serveIndex(c, indexPath)
			return
		}

		fileInfo, err := fs.Stat(publicFS, requested)
		if err == nil && fileInfo.Mode().IsRegular() {
			// Never serve a symlink pointing outside the public Vite directory.
			if !withinRoot(absolute, requested) {
				c.Status(http.StatusNotFound)
				return
			}
			if strings.HasPrefix(requested, "assets/") {
				// Vite fingerprints production assets in dist/assets/.
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// Files that are missing must be 404, not HTML (otherwise a missing
		// JS chunk is returned as index.html and yields MIME type errors).
		// Unknown client-side routes without extensions fall back to React.
		if path.Ext(requested) != "" || strings.HasPrefix(requested, "assets/") || err == nil {
			c.Status(http.StatusNotFound)
			return
		}
		serveIndex(c, indexPath)
	})
	return nil
}

func serveIndex(c *gin.Context, indexPath string) {
	html, err := os.ReadFile(indexPath)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func safeURLPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") && part != "" {
			return false
		}
	}
	return true
}

func withinRoot(root, requested string) bool {
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(requested)))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
