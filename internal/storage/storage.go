package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Storage struct {
	DB *sql.DB
}

func Open(ctx context.Context, path string) (*Storage, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("database path: %w", err)
	}

	if err := os.MkdirAll(
		filepath.Dir(absPath), 0700,
	); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	f, err := os.OpenFile(absPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create private database: %w", err)
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(absPath, 0600); err != nil {
		return nil, fmt.Errorf("protect database: %w", err)
	}
	u := url.URL{
		Scheme: "file",
		Path:   absPath,
	}

	q := u.Query()
	q.Set("_journal_mode", "WAL")
	q.Set("_busy_timeout", "5000")
	q.Set("_foreign_keys", "on")
	q.Set("_synchronous", "NORMAL")
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return &Storage{DB: db}, nil
}

func (s *Storage) Migrate(ctx context.Context) error {
	var version int

	if err := s.DB.QueryRowContext(
		ctx, "PRAGMA user_version",
	).Scan(&version); err != nil {
		return err
	}

	if version != 0 && version != 1 {
		return fmt.Errorf(
			"unsupported schema version: %d",
			version,
		)
	}

	files := []string{
		"migrations/001_schema.sql",
	}

	for i := version; i < len(files); i++ {
		sqlBytes, err := migrationFiles.ReadFile(files[i])
		if err != nil {
			return err
		}

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(
			ctx, string(sqlBytes),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf(
				"migration %d: %w", i+1, err,
			)
		}

		_, err = tx.ExecContext(
			ctx,
			fmt.Sprintf("PRAGMA user_version = %d", i+1),
		)
		if err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Storage) Close() error {
	return s.DB.Close()
}
