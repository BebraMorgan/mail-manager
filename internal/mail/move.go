package mail

import (
	"context"
	"errors"
	"fmt"
	"github.com/emersion/go-imap/v2"
	"mail-manager/internal/storage"
	"time"
)

var ErrMoveUnsupported = errors.New("IMAP server does not support safe MOVE")
var ErrUIDValidityChanged = errors.New("mailbox UIDVALIDITY changed; resync before deleting")

// MoveToTrash is invoked ONLY by an authorized human API handler, never by worker/LLM.
// It deliberately refuses the library's COPY/STORE/EXPUNGE fallback.
func (m *Client) MoveToTrash(ctx context.Context, e storage.Email) error {
	cfg, _ := m.Settings()
	if cfg.TrashFolder == "" || cfg.TrashFolder == cfg.Folder {
		return errors.New("trash folder not configured")
	}
	if e.Account != cfg.Username || e.Folder != cfg.Folder {
		return errors.New("different mailbox")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	// The go-imap client may automatically fall back to a plain EXPUNGE when
	// MOVE is absent; that can expunge other messages. Require native MOVE.
	if !client.Caps().Has(imap.CapMove) && !client.Caps().Has(imap.CapIMAP4rev2) {
		return ErrMoveUnsupported
	}
	sel, err := client.Select(e.Folder, &imap.SelectOptions{ReadOnly: false}).Wait()
	if err != nil {
		return fmt.Errorf("select mailbox: %w", err)
	}
	if sel.UIDValidity != e.UIDValidity {
		return ErrUIDValidityChanged
	}
	set := imap.UIDSetNum(imap.UID(e.UID))
	var exact imap.UIDSet
	exact.AddNum(imap.UID(e.UID))
	result, err := client.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{exact}}, nil).Wait()
	if err != nil {
		return err
	}
	if len(result.AllUIDs()) != 1 || result.AllUIDs()[0] != imap.UID(e.UID) {
		return ErrMessageGone
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = client.Move(set, cfg.TrashFolder).Wait()
	if err != nil {
		return fmt.Errorf("IMAP MOVE: %w", err)
	}
	return nil
}

type FolderInfo struct {
	Name       string             `json:"name"`
	Attributes []imap.MailboxAttr `json:"attributes"`
}

func (m *Client) ListFolders(ctx context.Context) ([]FolderInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	items, err := client.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]FolderInfo, 0, len(items))
	for _, x := range items {
		out = append(out, FolderInfo{Name: x.Mailbox, Attributes: x.Attrs})
	}
	return out, nil
}
