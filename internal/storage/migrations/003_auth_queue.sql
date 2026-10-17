-- Compatible with schema v2: preserve existing emails and classifications.
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin','viewer')),
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    token_version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);
ALTER TABLE classification_jobs ADD COLUMN lease_token TEXT NOT NULL DEFAULT '';
ALTER TABLE classification_jobs ADD COLUMN lease_until INTEGER NOT NULL DEFAULT 0;
ALTER TABLE classifications ADD COLUMN body_truncated INTEGER NOT NULL DEFAULT 0;
ALTER TABLE classifications ADD COLUMN original_chars INTEGER NOT NULL DEFAULT 0;
ALTER TABLE classifications ADD COLUMN processed_chars INTEGER NOT NULL DEFAULT 0;
ALTER TABLE classifications ADD COLUMN needs_review INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_jobs_lease ON classification_jobs(status, lease_until);
