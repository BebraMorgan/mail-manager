package mail

import (
	"mail-manager/internal/config"
	"sync"
	"time"
)

type Message struct {
	Account     string
	Folder      string
	UIDValidity uint32
	UID         uint32
	Sender      string
	Subject     string
	ReceivedAt  time.Time
	Body        string
}
type Client struct {
	mu         sync.RWMutex
	config     config.MailConfig
	configured bool
}

func New(cfg config.MailConfig) *Client { return &Client{config: cfg} }

// PublicSettings excludes OAuth tokens and app passwords from API wiring.
type PublicSettings struct {
	Username    string
	Folder      string
	TrashFolder string
	AllowDelete bool
}

func (m *Client) Configure(cfg config.MailConfig) {
	m.mu.Lock()
	m.config = cfg
	m.configured = true
	m.mu.Unlock()
}
func (m *Client) Settings() (config.MailConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config, m.configured
}
func (m *Client) PublicSettings() PublicSettings {
	cfg, _ := m.Settings()
	return PublicSettings{Username: cfg.Username, Folder: cfg.Folder, TrashFolder: cfg.TrashFolder, AllowDelete: cfg.AllowDelete}
}

func (m *Client) maxMessageBytes() int { cfg, _ := m.Settings(); return cfg.MaxMessageBytes }
