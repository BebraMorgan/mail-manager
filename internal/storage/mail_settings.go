package storage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mail-manager/internal/config"
	"os"
	"time"
)

var ErrMailNotConfigured = errors.New("mail settings not configured")

func mailCipher() (cipher.AEAD, error) {
	key, err := hex.DecodeString(os.Getenv("MAIL_SETTINGS_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("MAIL_SETTINGS_KEY must be 64 hex chars")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
func ValidateMailConfig(c config.MailConfig) error {
	if (c.Host != "imap.yandex.ru" && c.Host != "imap.yandex.com") || c.Username == "" || c.Port < 1 || c.Port > 65535 || c.Folder == "" || c.MaxMessageBytes < 1024 || c.FetchLimit < 1 || c.FetchLimit > 500 || c.PollInterval <= 0 {
		return errors.New("invalid IMAP settings")
	}
	if c.AuthMethod == "oauth2" && c.OAuthToken == "" || c.AuthMethod == "app_password" && c.AppPassword == "" || c.AuthMethod != "oauth2" && c.AuthMethod != "app_password" {
		return errors.New("missing or invalid IMAP credentials")
	}
	if c.AllowDelete && (c.TrashFolder == "" || c.TrashFolder == c.Folder) {
		return errors.New("trash folder must differ from inbox")
	}
	return nil
}
func (s *Storage) SaveMailSettings(ctx context.Context, c config.MailConfig) error {
	if err := ValidateMailConfig(c); err != nil {
		return err
	}
	var incompatible int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM emails WHERE account <> ? OR folder <> ?`, c.Username, c.Folder).Scan(&incompatible); err != nil {
		return err
	}
	if incompatible > 0 {
		return errors.New("account/folder change requires a fresh database")
	}
	a, err := mailCipher()
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	encrypted := a.Seal(nonce, nonce, data, nil)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO mail_settings(id,encrypted_config,updated_at) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET encrypted_config=excluded.encrypted_config,updated_at=excluded.updated_at`, encrypted, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Storage) LoadMailSettings(ctx context.Context) (config.MailConfig, error) {
	var c config.MailConfig
	var data []byte
	err := s.DB.QueryRowContext(ctx, `SELECT encrypted_config FROM mail_settings WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrMailNotConfigured
	}
	if err != nil {
		return c, err
	}
	a, err := mailCipher()
	if err != nil {
		return c, err
	}
	if len(data) < a.NonceSize() {
		return c, errors.New("malformed encrypted mail settings")
	}
	clear, err := a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], nil)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(clear, &c)
	return c, err
}
