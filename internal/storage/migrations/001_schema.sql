CREATE TABLE IF NOT EXISTS emails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    account TEXT NOT NULL,
    folder TEXT NOT NULL DEFAULT 'INBOX',
    uid_validity INTEGER NOT NULL,
    uid INTEGER NOT NULL,

    sender TEXT NOT NULL,
    subject TEXT NOT NULL,
    received_at TEXT,
    deletion_state TEXT NOT NULL DEFAULT 'active' CHECK(deletion_state IN ('active','moving','deleted','unknown')),
    deleted_at TEXT,
    deleted_by INTEGER,
    created_at TEXT NOT NULL,

    UNIQUE(account, folder, uid_validity, uid),

    CHECK(uid_validity >= 0),
    CHECK(uid > 0)
);

CREATE TABLE IF NOT EXISTS classifications (
    email_id INTEGER PRIMARY KEY,

    spam_verdict TEXT NOT NULL DEFAULT 'not_spam' CHECK(spam_verdict IN ('spam','not_spam','uncertain')),
    action_required INTEGER NOT NULL DEFAULT 0 CHECK(action_required IN (0,1)),
    action_type TEXT NOT NULL DEFAULT 'none',
    summary TEXT NOT NULL DEFAULT '',
    manual_override INTEGER NOT NULL DEFAULT 0 CHECK(manual_override IN (0,1)),
    reviewed_by INTEGER,
    reviewed_at TEXT,
    body_truncated INTEGER NOT NULL DEFAULT 0,
    original_chars INTEGER NOT NULL DEFAULT 0,
    processed_chars INTEGER NOT NULL DEFAULT 0,
    needs_review INTEGER NOT NULL DEFAULT 0,
    is_spam INTEGER NOT NULL
        CHECK(is_spam IN (0, 1)),

    importance TEXT NOT NULL
        CHECK(importance IN (
            'low', 'medium', 'high', 'uncertain'
        )),

    include_in_report INTEGER NOT NULL
        CHECK(include_in_report IN (0, 1)),

    category TEXT NOT NULL,
    reason TEXT NOT NULL,
    model TEXT NOT NULL,
    classified_at TEXT NOT NULL,

    FOREIGN KEY (email_id)
        REFERENCES emails(id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_emails_received
    ON emails(received_at DESC);

CREATE INDEX IF NOT EXISTS idx_classifications_report
    ON classifications(include_in_report, importance);

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
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
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

CREATE TABLE users (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active = 1),
    token_version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);
CREATE TABLE refresh_sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_version INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX idx_refresh_expiry ON refresh_sessions(expires_at);
CREATE TABLE mail_settings (
    id INTEGER PRIMARY KEY CHECK(id=1),
    encrypted_config BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mail_delete_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email_id INTEGER NOT NULL,
    actor_user_id INTEGER NOT NULL,
    action TEXT NOT NULL DEFAULT 'move_to_trash',
    status TEXT NOT NULL CHECK(status IN ('started','success','failed','uncertain')),
    error_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY(email_id) REFERENCES emails(id),
    FOREIGN KEY(actor_user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_delete_audit_email ON mail_delete_audit(email_id, id DESC);

CREATE INDEX idx_jobs_lease ON classification_jobs(status,lease_until);
CREATE INDEX idx_emails_delete_state ON emails(deletion_state,id);
CREATE INDEX idx_classification_filter ON classifications(category,importance,spam_verdict,include_in_report);
