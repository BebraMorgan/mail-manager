CREATE TABLE IF NOT EXISTS emails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    account TEXT NOT NULL,
    folder TEXT NOT NULL DEFAULT 'INBOX',
    uid_validity INTEGER NOT NULL,
    uid INTEGER NOT NULL,

    sender TEXT NOT NULL,
    subject TEXT NOT NULL,
    received_at TEXT,
    created_at TEXT NOT NULL,

    UNIQUE(account, folder, uid_validity, uid),

    CHECK(uid_validity >= 0),
    CHECK(uid > 0)
);

CREATE TABLE IF NOT EXISTS classifications (
    email_id INTEGER PRIMARY KEY,

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
