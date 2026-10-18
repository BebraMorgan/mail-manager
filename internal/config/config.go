package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App        AppConfig
	Database   DatabaseConfig
	Mail       MailConfig
	LLM        LLMConfig
	Classifier ClassifierConfig
	Auth       AuthConfig
}

type AuthConfig struct {
	JWTSecret string
	JWTIssuer string
	TokenTTL  time.Duration
}

type AppConfig struct {
	Env          string
	Host         string
	Port         int
	Mode         string
	FrontendDist string
}

type DatabaseConfig struct {
	Path string
}

type MailConfig struct {
	Host            string
	Port            int
	Username        string
	AuthMethod      string
	OAuthToken      string
	AppPassword     string
	Folder          string
	PollInterval    time.Duration
	FetchLimit      int
	MaxMessageBytes int
	TrashFolder     string
	AllowDelete     bool
}

type LLMConfig struct {
	BaseURL        string
	Model          string
	APIKey         string
	Timeout        time.Duration
	Temperature    float64
	MaxTokens      int
	MaxConcurrency int
}

type ClassifierConfig struct {
	MaxEmailChars       int
	IncludeSpamInReport bool
}

func Load() (*Config, error) {
	err := godotenv.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	cfg := &Config{
		Auth: AuthConfig{JWTSecret: get("JWT_SECRET", ""), JWTIssuer: get("JWT_ISSUER", "mail-manager")},
		App: AppConfig{
			Env:          get("APP_ENV", "development"),
			Host:         get("APP_HOST", "127.0.0.1"),
			Mode:         get("GIN_MODE", "debug"),
			FrontendDist: get("FRONTEND_DIST", ""),
		},
		Database: DatabaseConfig{
			Path: get("DB_PATH", "./data/mail-manager.db"),
		},
		Mail: MailConfig{Host: "imap.yandex.ru", Port: 993, AuthMethod: "oauth2", Folder: "INBOX", PollInterval: 5 * time.Minute, FetchLimit: 30, MaxMessageBytes: 1048576},
		LLM: LLMConfig{
			BaseURL: get("LLM_BASE_URL", "http://127.0.0.1:8080/v1"),
			Model:   get("LLM_MODEL", "qwen3-4b"),
			APIKey:  get("LLM_API_KEY", "local"),
		},
	}

	if cfg.Auth.TokenTTL, err = durationEnv("JWT_TOKEN_TTL", "15m"); err != nil {
		return nil, err
	}
	if cfg.App.Port, err = intEnv("APP_PORT", 3000); err != nil {
		return nil, err
	}

	if cfg.LLM.Timeout, err = durationEnv("LLM_TIMEOUT", "120s"); err != nil {
		return nil, err
	}
	if cfg.LLM.Temperature, err = floatEnv("LLM_TEMPERATURE", 0); err != nil {
		return nil, err
	}
	if cfg.LLM.MaxTokens, err = intEnv("LLM_MAX_TOKENS", 192); err != nil {
		return nil, err
	}
	if cfg.LLM.MaxConcurrency, err = intEnv("LLM_MAX_CONCURRENCY", 1); err != nil {
		return nil, err
	}
	if cfg.Classifier.MaxEmailChars, err = intEnv("CLASSIFIER_MAX_EMAIL_CHARS", 6000); err != nil {
		return nil, err
	}
	if cfg.Classifier.IncludeSpamInReport, err = boolEnv(
		"CLASSIFIER_INCLUDE_SPAM_IN_REPORT", false,
	); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if len(c.Auth.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must contain at least 32 bytes")
	}
	if c.Auth.JWTIssuer == "" || c.Auth.TokenTTL < time.Minute || c.Auth.TokenTTL > 24*time.Hour {
		return fmt.Errorf("invalid JWT configuration")
	}
	if ip := net.ParseIP(c.App.Host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("APP_HOST must be loopback; use HTTPS reverse proxy for remote access")
	}
	if c.App.Port < 1 || c.App.Port > 65535 {
		return fmt.Errorf("invalid APP_PORT")
	}

	switch c.App.Mode {
	case "debug", "release", "test":
	default:
		return fmt.Errorf("invalid GIN_MODE: %s", c.App.Mode)
	}

	if c.Database.Path == "" {
		return fmt.Errorf("DB_PATH is required")
	}

	if c.LLM.BaseURL == "" || c.LLM.Model == "" {
		return fmt.Errorf("LLM_BASE_URL and LLM_MODEL are required")
	}

	if c.LLM.Timeout <= 0 {
		return fmt.Errorf("LLM_TIMEOUT must be positive")
	}

	if math.IsNaN(c.LLM.Temperature) || math.IsInf(c.LLM.Temperature, 0) || c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
		return fmt.Errorf("invalid LLM_TEMPERATURE")
	}

	if c.LLM.MaxTokens < 1 || c.LLM.MaxConcurrency < 1 {
		return fmt.Errorf("invalid LLM limits")
	}

	if c.Classifier.MaxEmailChars < 1 {
		return fmt.Errorf("invalid CLASSIFIER_MAX_EMAIL_CHARS")
	}

	return nil
}

func get(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(value)
}

func intEnv(key string, fallback int) (int, error) {
	value := get(key, "")
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return result, nil
}

func floatEnv(key string, fallback float64) (float64, error) {
	value := get(key, "")
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return result, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := get(key, "")
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return result, nil
}

func durationEnv(key, fallback string) (time.Duration, error) {
	value := get(key, fallback)
	result, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return result, nil
}
