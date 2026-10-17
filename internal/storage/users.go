package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("user not found")
var ErrDuplicateUser = errors.New("user already exists")

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	Active       bool   `json:"active"`
	TokenVersion int64  `json:"-"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"created_at"`
}

func ValidateCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return errors.New("username must be 3-64 characters")
	}
	for _, r := range username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return errors.New("username contains forbidden characters")
		}
	}
	if len(password) < 12 || len(password) > 72 {
		return errors.New("password must be 12-72 bytes")
	}
	return nil
}
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}
func (s *Storage) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// BootstrapAdmin only succeeds while the DB has no users; protected by SQLite's write transaction.
func (s *Storage) BootstrapAdmin(ctx context.Context, username, password string) error {
	if err := ValidateCredentials(username, password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,created_at)
        SELECT ?,?,'admin',? WHERE NOT EXISTS(SELECT 1 FROM users)`, strings.ToLower(username), hash, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("bootstrap refused: users already exist")
	}
	return nil
}
func (s *Storage) CreateUser(ctx context.Context, username, password, role string) (*User, error) {
	if err := ValidateCredentials(username, password); err != nil {
		return nil, err
	}
	if role != "admin" && role != "viewer" {
		return nil, errors.New("invalid role")
	}
	h, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,created_at) VALUES(?,?,?,?)`,
		strings.ToLower(username), h, role, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrDuplicateUser
		}
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}
func (s *Storage) GetUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,username,password_hash,role,active,token_version,created_at FROM users WHERE id=?`, id).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Active, &u.TokenVersion, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Storage) FindUser(ctx context.Context, username string) (*User, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE username=? COLLATE NOCASE`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}
func (s *Storage) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,username,password_hash,role,active,token_version,created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Active, &u.TokenVersion, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
func (s *Storage) DisableUser(ctx context.Context, id, actorID int64) error {
	if id == actorID {
		return errors.New("cannot disable your own account")
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE users SET active=0,token_version=token_version+1 WHERE id=? AND active=1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}
func (s *Storage) ChangePassword(ctx context.Context, id int64, oldPassword, newPassword string) error {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("invalid current password")
	}
	if err = ValidateCredentials(u.Username, newPassword); err != nil {
		return err
	}
	h, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE users SET password_hash=?,token_version=token_version+1 WHERE id=? AND password_hash=?`, h, id, u.PasswordHash)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("password changed concurrently")
	}
	return nil
}
