package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/emersion/go-message/charset"
	"mime"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"mail-manager/internal/storage"
)

var (
	ErrMessageGone     = errors.New("message no longer exists")
	ErrMessageTooLarge = errors.New("message exceeds size limit")
)

const imapBatchSize = 20

// connect opens an encrypted IMAP connection with a bounded session lifetime.
// Cancellation closes the underlying socket and unblocks pending IMAP commands.
func (m *Client) connect(ctx context.Context) (*imapclient.Client, error) {
	cfg := m.config
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", address, &tls.Config{
		ServerName: cfg.Host, MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return nil, fmt.Errorf("IMAP TLS connection: %w", err)
	}
	// Each scan handles bounded batches. On a large mailbox, a session can
	// expire and the next scan resumes from the committed UID cursor.
	if err := conn.SetDeadline(time.Now().Add(3 * time.Minute)); err != nil {
		conn.Close()
		return nil, err
	}
	client := imapclient.New(conn, &imapclient.Options{WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader}})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-client.Closed():
		}
	}()
	if err := client.WaitGreeting(); err != nil {
		client.Close()
		return nil, fmt.Errorf("IMAP greeting: %w", err)
	}
	if err := authenticate(client, cfg); err != nil {
		client.Close()
		return nil, fmt.Errorf("IMAP auth: %w", err)
	}
	return client, nil
}

// Scan registers new emails without downloading their bodies.
// The mailbox is always opened in read-only mode.
func (m *Client) Scan(
	ctx context.Context,
	db *storage.Storage,
) (int, error) {
	cfg := m.config

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	client, err := m.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	selected, err := client.Select(
		cfg.Folder,
		&imap.SelectOptions{
			ReadOnly: true,
		},
	).Wait()
	if err != nil {
		return 0, fmt.Errorf("select mailbox: %w", err)
	}

	if selected.UIDValidity == 0 {
		return 0, fmt.Errorf("missing UIDVALIDITY")
	}

	lastUID, err := db.GetCursor(
		ctx,
		cfg.Username,
		cfg.Folder,
		selected.UIDValidity,
	)
	if err != nil {
		return 0, fmt.Errorf("read IMAP cursor: %w", err)
	}

	if lastUID == ^uint32(0) {
		return 0, nil
	}

	// Search for UIDs newer than the saved cursor.
	criteria := &imap.SearchCriteria{}

	if lastUID > 0 {
		var uidSet imap.UIDSet
		uidSet.AddRange(imap.UID(lastUID+1), 0)
		criteria.UID = []imap.UIDSet{uidSet}
	}

	result, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return 0, fmt.Errorf("UID SEARCH: %w", err)
	}

	uids := result.AllUIDs()

	// Never move the cursor backwards.
	filtered := make([]imap.UID, 0, len(uids))
	for _, uid := range uids {
		if uint32(uid) > lastUID {
			filtered = append(filtered, uid)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i] < filtered[j]
	})

	if len(filtered) == 0 {
		return 0, nil
	}

	registered := 0

	batchSize := imapBatchSize
	if cfg.FetchLimit > 0 && cfg.FetchLimit < batchSize {
		batchSize = cfg.FetchLimit
	}
	for start := 0; start < len(filtered); start += batchSize {
		if err := ctx.Err(); err != nil {
			return registered, err
		}

		end := start + batchSize
		if end > len(filtered) {
			end = len(filtered)
		}

		page := filtered[start:end]

		// One UID FETCH per batch.
		messages, err := client.Fetch(
			imap.UIDSetNum(page...),
			&imap.FetchOptions{
				UID:          true,
				Envelope:     true,
				InternalDate: true,
				RFC822Size:   true,
			},
		).Collect()

		if err != nil {
			// Do not advance cursor on failed FETCH.
			return registered, fmt.Errorf(
				"UID FETCH metadata failed "+
					"(UID %d-%d, folder %s): %w",
				page[0],
				page[len(page)-1],
				cfg.Folder,
				err,
			)
		}

		// Build a map because FETCH responses do not
		// have to be returned in UID order.
		byUID := make(map[imap.UID]*imapclient.FetchMessageBuffer)

		for _, msg := range messages {
			if msg.UID != 0 {
				byUID[msg.UID] = msg
			}
		}

		batch := make([]storage.Email, 0, len(page))

		for _, uid := range page {
			msg, ok := byUID[uid]

			// A message can disappear between SEARCH
			// and FETCH if it was deleted elsewhere.
			if !ok {
				// Search the exact UID again: only a confirmed expunge can be skipped.
				var exact imap.UIDSet
				exact.AddNum(uid)
				verify, verifyErr := client.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{exact}}, nil).Wait()
				if verifyErr != nil {
					return registered, fmt.Errorf("verify missing UID %d: %w", uid, verifyErr)
				}
				if len(verify.AllUIDs()) > 0 {
					return registered, fmt.Errorf("UID %d is still present but FETCH omitted it", uid)
				}
				continue
			}

			if msg.Envelope == nil {
				// Do not skip malformed metadata silently.
				return registered, fmt.Errorf(
					"missing envelope for UID %d", uid,
				)
			}

			sender := ""

			if len(msg.Envelope.From) > 0 {
				from := msg.Envelope.From[0]
				sender = from.Mailbox + "@" + from.Host
			}

			batch = append(batch, storage.Email{
				Account:     cfg.Username,
				Folder:      cfg.Folder,
				UIDValidity: selected.UIDValidity,
				UID:         uint32(uid),
				Sender:      sender,
				Subject:     msg.Envelope.Subject,
				ReceivedAt:  msg.InternalDate,
			})
		}

		// Update cursor only after successful FETCH.
		highest := uint32(page[len(page)-1])

		err = db.Enqueue(
			ctx,
			batch,
			cfg.Username,
			cfg.Folder,
			selected.UIDValidity,
			highest,
		)
		if err != nil {
			return registered, fmt.Errorf(
				"save IMAP batch: %w", err,
			)
		}

		registered += len(batch)
		lastUID = highest
	}

	return registered, nil
}

// FetchOne retrieves one queued message body.
// It never changes the message's read status.
func (m *Client) FetchOne(
	ctx context.Context,
	email storage.Email,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	client, err := m.connect(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()

	selected, err := client.Select(
		email.Folder,
		&imap.SelectOptions{ReadOnly: true},
	).Wait()
	if err != nil {
		return "", fmt.Errorf("select mailbox: %w", err)
	}

	// UID may no longer identify the same message.
	if selected.UIDValidity != email.UIDValidity {
		return "", ErrMessageGone
	}

	uid := imap.UID(email.UID)

	// Fetch size first to avoid downloading
	// large attachments into RAM.
	metadata, err := client.Fetch(
		imap.UIDSetNum(uid),
		&imap.FetchOptions{
			UID:        true,
			RFC822Size: true,
		},
	).Collect()

	if err != nil {
		return "", fmt.Errorf(
			"UID FETCH size %d: %w", uid, err,
		)
	}

	if len(metadata) == 0 {
		return "", ErrMessageGone
	}

	if int64(metadata[0].RFC822Size) >
		int64(m.config.MaxMessageBytes) {
		return "", ErrMessageTooLarge
	}

	// PEEK prevents setting the \Seen flag.
	section := &imap.FetchItemBodySection{
		Peek: true,
	}

	messages, err := client.Fetch(
		imap.UIDSetNum(uid),
		&imap.FetchOptions{
			UID: true,
			BodySection: []*imap.FetchItemBodySection{
				section,
			},
		},
	).Collect()

	if err != nil {
		return "", fmt.Errorf(
			"UID FETCH body %d: %w", uid, err,
		)
	}

	if len(messages) == 0 {
		return "", ErrMessageGone
	}

	raw := messages[0].FindBodySection(section)

	if len(raw) == 0 {
		return "", ErrMessageGone
	}

	if len(raw) > m.config.MaxMessageBytes {
		return "", ErrMessageTooLarge
	}

	body, err := ParseBody(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf(
			"parse message UID %d: %w", uid, err,
		)
	}

	return body, nil
}
