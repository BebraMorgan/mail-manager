package api

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"mail-manager/internal/auth"
	"mail-manager/internal/storage"
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
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"access_token": token, "token_type": "Bearer"})
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
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		id, version, err := a.tokens.Verify(strings.TrimPrefix(h, "Bearer "))
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
func CurrentUser(c *gin.Context) *storage.User {
	v, ok := c.Get("user")
	if !ok {
		return nil
	}
	u, _ := v.(*storage.User)
	return u
}
func (a *APIAuth) AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil || u.Role != "admin" {
			c.AbortWithStatusJSON(403, gin.H{"error": "admin required"})
			return
		}
		c.Next()
	}
}
func (a *APIAuth) Me(c *gin.Context) { c.JSON(200, CurrentUser(c)) }
func (a *APIAuth) ListUsers(c *gin.Context) {
	users, err := a.db.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "database error"})
		return
	}
	c.JSON(200, gin.H{"items": users})
}
func (a *APIAuth) CreateUser(c *gin.Context) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid JSON"})
		return
	}
	u, err := a.db.CreateUser(c.Request.Context(), body.Username, body.Password, body.Role)
	if errors.Is(err, storage.ErrDuplicateUser) {
		c.JSON(409, gin.H{"error": "user exists"})
		return
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, u)
}
func (a *APIAuth) DisableUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid user ID"})
		return
	}
	err = a.db.DisableUser(c.Request.Context(), id, CurrentUser(c).ID)
	if errors.Is(err, storage.ErrNotFound) {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.Status(204)
}
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
	c.Status(204) // invalidates every previously issued access token
}
