package config

import (
	"errors"
	"fmt"
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
}

type AppConfig struct {
	Env  string
	Host string
	Port int
	Mode string
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
		App: AppConfig{
			Env:  get("APP_ENV", "development"),
			Host: get("APP_HOST", "127.0.0.1"),
			Mode: get("GIN_MODE", "debug"),
		},
		Database: DatabaseConfig{
			Path: get("DB_PATH", "./data/mail-manager.db"),
		},
		Mail: MailConfig{
			Host:        get("MAIL_HOST", "imap.yandex.ru"),
			Username:    get("MAIL_USERNAME", ""),
			AuthMethod:  get("MAIL_AUTH_METHOD", "oauth2"),
			OAuthToken:  get("MAIL_OAUTH_TOKEN", ""),
			AppPassword: get("MAIL_APP_PASSWORD", ""),
			Folder:      get("MAIL_FOLDER", "INBOX"),
		},
		LLM: LLMConfig{
			BaseURL: get("LLM_BASE_URL", "http://127.0.0.1:8080/v1"),
			Model:   get("LLM_MODEL", "qwen3-4b"),
			APIKey:  get("LLM_API_KEY", "local"),
		},
	}

	if cfg.App.Port, err = intEnv("APP_PORT", 3000); err != nil {
		return nil, err
	}
	if cfg.Mail.Port, err = intEnv("MAIL_PORT", 993); err != nil {
		return nil, err
	}
	if cfg.Mail.PollInterval, err = durationEnv("MAIL_POLL_INTERVAL", "5m"); err != nil {
		return nil, err
	}
	if cfg.Mail.FetchLimit, err = intEnv(
		"MAIL_FETCH_LIMIT", 30,
	); err != nil {
		return nil, err
	}

	if cfg.Mail.MaxMessageBytes, err = intEnv(
		"MAIL_MAX_MESSAGE_BYTES", 1048576,
	); err != nil {
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

	if c.Mail.Port < 1 || c.Mail.Port > 65535 {
		return fmt.Errorf("invalid MAIL_PORT")
	}

	if c.Mail.AuthMethod != "oauth2" &&
		c.Mail.AuthMethod != "app_password" {
		return fmt.Errorf("invalid MAIL_AUTH_METHOD")
	}

	if c.Mail.PollInterval <= 0 {
		return fmt.Errorf("MAIL_POLL_INTERVAL must be positive")
	}

	if c.Mail.FetchLimit < 1 || c.Mail.FetchLimit > 500 {
		return fmt.Errorf("MAIL_FETCH_LIMIT must be 1..500")
	}

	if c.Mail.MaxMessageBytes < 1024 {
		return fmt.Errorf("MAIL_MAX_MESSAGE_BYTES must be >= 1024")
	}

	if c.LLM.BaseURL == "" || c.LLM.Model == "" {
		return fmt.Errorf("LLM_BASE_URL and LLM_MODEL are required")
	}

	if c.LLM.Timeout <= 0 {
		return fmt.Errorf("LLM_TIMEOUT must be positive")
	}

	if c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
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
