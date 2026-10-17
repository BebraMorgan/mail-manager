// Package auth issues and validates short-lived HS256 access tokens.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid access token")

type Manager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

type Claims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
	Version   int64  `json:"ver"`
	ID        string `json:"jti"`
}

func New(secret, issuer string, ttl time.Duration) (*Manager, error) {
	if len(secret) < 32 || issuer == "" || ttl <= 0 || ttl > 24*time.Hour {
		return nil, errors.New("invalid JWT configuration")
	}
	return &Manager{secret: []byte(secret), issuer: issuer, ttl: ttl}, nil
}
func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (m *Manager) Sign(userID, version int64) (string, error) {
	if userID <= 0 || version <= 0 {
		return "", errors.New("invalid subject")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	now := time.Now().Unix()
	header, err := encode(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := encode(Claims{Subject: strconv.FormatInt(userID, 10), Issuer: m.issuer, Audience: "mail-manager-api",
		IssuedAt: now, NotBefore: now, ExpiresAt: now + int64(m.ttl.Seconds()), Version: version,
		ID: base64.RawURLEncoding.EncodeToString(nonce[:])})
	if err != nil {
		return "", err
	}
	payload := header + "." + claims
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (m *Manager) Verify(token string) (int64, int64, error) {
	if len(token) > 8192 {
		return 0, 0, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, 0, ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != sha256.Size {
		return 0, 0, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return 0, 0, ErrInvalidToken
	}
	headBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, 0, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err = json.Unmarshal(headBytes, &header); err != nil || header.Alg != "HS256" || header.Typ != "JWT" {
		return 0, 0, ErrInvalidToken
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, 0, ErrInvalidToken
	}
	var claims Claims
	if err = json.Unmarshal(body, &claims); err != nil {
		return 0, 0, ErrInvalidToken
	}
	now := time.Now().Unix()
	if claims.Issuer != m.issuer || claims.Audience != "mail-manager-api" || claims.Version <= 0 ||
		claims.IssuedAt > now+30 || claims.NotBefore > now+30 || claims.ExpiresAt <= now ||
		claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt-claims.IssuedAt > int64(m.ttl.Seconds())+1 ||
		claims.ID == "" {
		return 0, 0, ErrInvalidToken
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, 0, fmt.Errorf("%w: subject", ErrInvalidToken)
	}
	return id, claims.Version, nil
}
