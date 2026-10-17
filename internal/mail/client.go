package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"mail-manager/internal/config"
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
	config config.MailConfig
}

func New(cfg config.MailConfig) *Client {
	return &Client{config: cfg}
}

func (m *Client) FetchRecent(
	ctx context.Context,
	process func(context.Context, Message) error,
) error {
	cfg := m.config

	address := net.JoinHostPort(
		cfg.Host,
		strconv.Itoa(cfg.Port),
	)

	client, err := imapclient.DialTLS(
		address,
		&imapclient.Options{
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: cfg.Host,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("connect to IMAP: %w", err)
	}
	defer client.Close()

	if err := authenticate(client, cfg); err != nil {
		return fmt.Errorf("IMAP authentication: %w", err)
	}

	selected, err := client.Select(
		cfg.Folder,
		&imap.SelectOptions{ReadOnly: true},
	).Wait()
	if err != nil {
		return fmt.Errorf("open mailbox read-only: %w", err)
	}
	if selected.UIDValidity == 0 {
		return fmt.Errorf("IMAP mailbox has no UIDVALIDITY")
	}
	if selected.NumMessages == 0 {
		return nil
	}

	searchResult, err := client.UIDSearch(
		&imap.SearchCriteria{},
		nil,
	).Wait()
	if err != nil {
		return fmt.Errorf("search messages: %w", err)
	}

	uids := searchResult.AllUIDs()

	sort.Slice(uids, func(i, j int) bool {
		return uids[i] > uids[j]
	})

	if len(uids) > cfg.FetchLimit {
		uids = uids[:cfg.FetchLimit]
	}

	section := &imap.FetchItemBodySection{
		Peek: true,
	}

	for _, uid := range uids {
		if err := ctx.Err(); err != nil {
			return err
		}

		meta, err := client.Fetch(
			imap.UIDSetNum(uid),
			&imap.FetchOptions{
				UID:          true,
				Envelope:     true,
				InternalDate: true,
				RFC822Size:   true,
			},
		).Collect()
		if err != nil {
			return fmt.Errorf("fetch metadata: %w", err)
		}
		if len(meta) == 0 || meta[0].Envelope == nil {
			continue
		}

		info := meta[0]

		if int64(info.RFC822Size) > int64(cfg.MaxMessageBytes) {
			continue
		}

		fetched, err := client.Fetch(
			imap.UIDSetNum(uid),
			&imap.FetchOptions{
				UID:         true,
				BodySection: []*imap.FetchItemBodySection{section},
			},
		).Collect()
		if err != nil {
			return fmt.Errorf("fetch body: %w", err)
		}
		if len(fetched) == 0 {
			continue
		}

		raw := fetched[0].FindBodySection(section)
		if len(raw) == 0 ||
			len(raw) > cfg.MaxMessageBytes {
			continue
		}

		body, err := ParseBody(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("parse UID %d: %w", uid, err)
		}

		sender := ""
		if len(info.Envelope.From) > 0 {
			from := info.Envelope.From[0]
			sender = from.Mailbox + "@" + from.Host
		}

		msg := Message{
			Account:     cfg.Username,
			Folder:      cfg.Folder,
			UIDValidity: selected.UIDValidity,
			UID:         uint32(uid),
			Sender:      sender,
			Subject:     info.Envelope.Subject,
			ReceivedAt:  info.InternalDate,
			Body:        body,
		}

		if err := process(ctx, msg); err != nil {
			return fmt.Errorf(
				"process UID %d: %w", uid, err,
			)
		}
	}

	return nil
}
