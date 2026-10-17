package mail

import (
	"mail-manager/internal/config"
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
type Client struct{ config config.MailConfig }

func New(cfg config.MailConfig) *Client { return &Client{config: cfg} }

// PublicSettings excludes OAuth tokens and app passwords from API wiring.
type PublicSettings struct {
	Username    string
	Folder      string
	TrashFolder string
	AllowDelete bool
}

func (m *Client) PublicSettings() PublicSettings {
	return PublicSettings{Username: m.config.Username, Folder: m.config.Folder, TrashFolder: m.config.TrashFolder, AllowDelete: m.config.AllowDelete}
}
