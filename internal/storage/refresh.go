package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var ErrInvalidRefresh = errors.New("invalid refresh session")

func tokenHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return hex.EncodeToString(v[:])
}
func (s *Storage) StoreRefresh(ctx context.Context, token string, id, version int64, expiry time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO refresh_sessions(token_hash,user_id,token_version,expires_at) VALUES(?,?,?,?)`, tokenHash(token), id, version, expiry.Unix())
	return err
}
func (s *Storage) RotateRefresh(ctx context.Context, oldToken, newToken string, id, version int64, expires time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM refresh_sessions WHERE token_hash=? AND user_id=? AND token_version=? AND expires_at>?`, tokenHash(oldToken), id, version, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidRefresh
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO refresh_sessions(token_hash,user_id,token_version,expires_at) VALUES(?,?,?,?)`, tokenHash(newToken), id, version, expires.Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Storage) ReadRefresh(ctx context.Context, token string) (int64, int64, error) {
	var id, v int64
	err := s.DB.QueryRowContext(ctx, `SELECT user_id,token_version FROM refresh_sessions WHERE token_hash=? AND expires_at>?`, tokenHash(token), time.Now().Unix()).Scan(&id, &v)
	if err != nil {
		return 0, 0, ErrInvalidRefresh
	}
	return id, v, nil
}
func (s *Storage) RevokeRefresh(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM refresh_sessions WHERE token_hash=?`, tokenHash(token))
	return err
}
