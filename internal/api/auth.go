package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"mail-manager/internal/auth"
	"mail-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type APIAuth struct {
	db       *storage.Storage
	tokens   *auth.Manager
	mu       sync.Mutex
	attempts map[string]loginAttempts
}
type loginAttempts struct {
	count int
	reset time.Time
}

func NewAPIAuth(db *storage.Storage, tokens *auth.Manager) *APIAuth {
	return &APIAuth{db: db, tokens: tokens, attempts: make(map[string]loginAttempts)}
}
func (a *APIAuth) Login(c *gin.Context) {
	var input struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "invalid credentials"})
		return
	}
	remote, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		remote = c.Request.RemoteAddr
	}
	key := remote + ":" + strings.ToLower(input.Username)
	if !a.loginAllowed(key) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
		return
	}
	user, err := a.db.FindUser(c.Request.Context(), input.Username)
	if err != nil || !user.Active || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)) != nil {
		// Uniform failure response; never disclose whether username exists.
		a.failLogin(key)
		c.JSON(401, gin.H{"error": "invalid credentials"})
		return
	}
	token, err := a.tokens.Sign(user.ID, user.TokenVersion)
	if err != nil {
		c.JSON(500, gin.H{"error": "token creation failed"})
		return
	}
	a.clearLogin(key)
	csrf, err := newCSRFToken()
	if err != nil {
		c.JSON(500, gin.H{"error": "session creation failed"})
		return
	}
	refresh, err := newCSRFToken()
	if err != nil {
		c.JSON(500, gin.H{"error": "session creation failed"})
		return
	}
	if err = a.db.StoreRefresh(c.Request.Context(), refresh, user.ID, user.TokenVersion, time.Now().Add(30*24*time.Hour)); err != nil {
		c.JSON(500, gin.H{"error": "session creation failed"})
		return
	}
	setSessionCookies(c, token, refresh, csrf)
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"status": "ok"})
}
func (a *APIAuth) loginAllowed(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if len(a.attempts) > 2048 {
		for k, v := range a.attempts {
			if now.After(v.reset) {
				delete(a.attempts, k)
			}
		}
	}
	entry, ok := a.attempts[key]
	if !ok && len(a.attempts) >= 4096 {
		return false
	}
	return !ok || now.After(entry.reset) || entry.count < 5
}
func (a *APIAuth) failLogin(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	entry := a.attempts[key]
	if now.After(entry.reset) {
		entry = loginAttempts{reset: now.Add(15 * time.Minute)}
	}
	entry.count++
	a.attempts[key] = entry
}
func (a *APIAuth) clearLogin(key string) { a.mu.Lock(); delete(a.attempts, key); a.mu.Unlock() }
func (a *APIAuth) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("mm_session")
		if err != nil || token == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		// Cookie authentication requires CSRF validation for state-changing requests.
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			csrfCookie, err := c.Cookie("mm_csrf")
			csrfHeader := c.GetHeader("X-CSRF-Token")
			if err != nil || csrfCookie == "" || csrfHeader == "" || subtle.ConstantTimeCompare([]byte(csrfCookie), []byte(csrfHeader)) != 1 {
				c.AbortWithStatusJSON(403, gin.H{"error": "invalid CSRF token"})
				return
			}
		}
		id, version, err := a.tokens.Verify(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		u, err := a.db.GetUser(c.Request.Context(), id)
		if err != nil || !u.Active || u.TokenVersion != version {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

func newCSRFToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func cookieSecure() bool { return os.Getenv("APP_ENV") == "production" }
func setSessionCookies(c *gin.Context, token, refresh, csrf string) {
	secure := cookieSecure()
	for _, cookie := range []*http.Cookie{
		{Name: "mm_session", Value: token, Path: "/api", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode},
		{Name: "mm_refresh", Value: refresh, Path: "/api/auth", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600},
		{Name: "mm_csrf", Value: csrf, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode},
	} {
		http.SetCookie(c.Writer, cookie)
	}
}
func (a *APIAuth) Logout(c *gin.Context) {
	if token, err := c.Cookie("mm_refresh"); err == nil {
		_ = a.db.RevokeRefresh(c.Request.Context(), token)
	}
	secure := cookieSecure()
	http.SetCookie(c.Writer, &http.Cookie{Name: "mm_session", Path: "/api", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(c.Writer, &http.Cookie{Name: "mm_refresh", Path: "/api/auth", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(c.Writer, &http.Cookie{Name: "mm_csrf", Path: "/", MaxAge: -1, Secure: secure, SameSite: http.SameSiteLaxMode})
	c.Status(204)
}
func CurrentUser(c *gin.Context) *storage.User {
	v, ok := c.Get("user")
	if !ok {
		return nil
	}
	u, _ := v.(*storage.User)
	return u
}
func (a *APIAuth) Me(c *gin.Context) { c.JSON(200, CurrentUser(c)) }
func (a *APIAuth) ChangePassword(c *gin.Context) {
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid JSON"})
		return
	}
	if err := a.db.ChangePassword(c.Request.Context(), CurrentUser(c).ID, body.Current, body.New); err != nil {
		c.JSON(400, gin.H{"error": "password change failed"})
		return
	}
	a.Logout(c) // password change invalidates all previous tokens
}

// Refresh rotates a single-use opaque refresh token and issues a new short-lived JWT.
func (a *APIAuth) Refresh(c *gin.Context) {
	if !validCSRF(c) {
		c.JSON(403, gin.H{"error": "invalid CSRF token"})
		return
	}
	old, err := c.Cookie("mm_refresh")
	if err != nil || old == "" {
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}
	id, version, err := a.db.ReadRefresh(c.Request.Context(), old)
	if err != nil {
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}
	u, err := a.db.GetUser(c.Request.Context(), id)
	if err != nil || !u.Active || u.TokenVersion != version {
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}
	next, err := newCSRFToken()
	if err != nil {
		c.JSON(500, gin.H{"error": "refresh failed"})
		return
	}
	if err = a.db.RotateRefresh(c.Request.Context(), old, next, id, version, time.Now().Add(30*24*time.Hour)); err != nil {
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}
	access, err := a.tokens.Sign(id, version)
	if err != nil {
		c.JSON(500, gin.H{"error": "refresh failed"})
		return
	}
	csrf, err := newCSRFToken()
	if err != nil {
		c.JSON(500, gin.H{"error": "refresh failed"})
		return
	}
	setSessionCookies(c, access, next, csrf)
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"status": "ok"})
}
func validCSRF(c *gin.Context) bool {
	cookie, err := c.Cookie("mm_csrf")
	header := c.GetHeader("X-CSRF-Token")
	return err == nil && cookie != "" && header != "" && subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

// LogoutWithRefresh supports logout even when the access token has expired.
func (a *APIAuth) LogoutWithRefresh(c *gin.Context) {
	if !validCSRF(c) {
		c.JSON(403, gin.H{"error": "invalid CSRF token"})
		return
	}
	a.Logout(c)
}
