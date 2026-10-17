package llm

import (
	"context"
	"net/http"

	openai "github.com/sashabaranov/go-openai"

	"mail-manager/internal/config"
)

type Client struct {
	api      *openai.Client
	cfg      config.LLMConfig
	limiter  chan struct{}
	maxChars int
}

func New(cfg config.LLMConfig, maxChars int) *Client {
	clientCfg := openai.DefaultConfig(cfg.APIKey)
	clientCfg.BaseURL = cfg.BaseURL
	clientCfg.HTTPClient = &http.Client{
		Timeout: cfg.Timeout,
	}

	return &Client{
		api:      openai.NewClientWithConfig(clientCfg),
		cfg:      cfg,
		limiter:  make(chan struct{}, cfg.MaxConcurrency),
		maxChars: maxChars,
	}
}

func (c *Client) acquire(ctx context.Context) error {
	select {
	case c.limiter <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() {
	<-c.limiter
}
