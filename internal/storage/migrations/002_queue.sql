CREATE TABLE IF NOT EXISTS mailbox_state (
    account TEXT NOT NULL,
    folder TEXT NOT NULL,
    uid_validity INTEGER NOT NULL,
    last_uid INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account, folder)
);

CREATE TABLE IF NOT EXISTS classification_jobs (
    email_id INTEGER PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN (
            'pending', 'processing', 'completed',
            'retry', 'skipped'
        )),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY(email_id) REFERENCES emails(id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_jobs_ready
    ON classification_jobs(
        status, next_attempt_at, email_id
    );

INSERT OR IGNORE INTO classification_jobs (
    email_id, status
)
SELECT
    e.id,
    CASE WHEN c.email_id IS NULL
        THEN 'pending'
        ELSE 'completed'
    END
FROM emails e
LEFT JOIN classifications c ON c.email_id = e.id;
