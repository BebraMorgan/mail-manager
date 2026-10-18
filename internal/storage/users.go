package storage

import (
	"context"
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("user not found")

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Active       bool   `json:"active"`
	TokenVersion int64  `json:"-"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"created_at"`
}

func ValidateCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || len(password) < 12 || len(password) > 72 {
		return errors.New("invalid username or password length")
	}
	for _, r := range username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return errors.New("invalid username")
		}
	}
	return nil
}
func (s *Storage) BootstrapAdmin(ctx context.Context, username, password string) error {
	if err := ValidateCredentials(username, password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,created_at) SELECT 1,?,?,? WHERE NOT EXISTS(SELECT 1 FROM users)`, strings.ToLower(username), string(hash), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("administrator already exists")
	}
	return nil
}
func (s *Storage) GetUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,username,password_hash,active,token_version,created_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Active, &u.TokenVersion, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Storage) FindUser(ctx context.Context, username string) (*User, error) {
	u, err := s.GetUser(ctx, 1)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(u.Username, username) {
		return nil, ErrNotFound
	}
	return u, nil
}
func (s *Storage) ChangePassword(ctx context.Context, id int64, oldPassword, newPassword string) error {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)) != nil {
		return errors.New("invalid password")
	}
	if err := ValidateCredentials(u.Username, newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,token_version=token_version+1 WHERE id=? AND password_hash=?`, string(hash), id, u.PasswordHash)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("password changed concurrently")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM refresh_sessions WHERE user_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
