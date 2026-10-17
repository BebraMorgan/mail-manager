-- Frontend classification fields, human review and safely audited mailbox operations.
ALTER TABLE emails ADD COLUMN deletion_state TEXT NOT NULL DEFAULT 'active' CHECK(deletion_state IN ('active','moving','deleted','unknown'));
ALTER TABLE emails ADD COLUMN deleted_at TEXT;
ALTER TABLE emails ADD COLUMN deleted_by INTEGER;
CREATE INDEX IF NOT EXISTS idx_emails_delete_state ON emails(deletion_state,id);

ALTER TABLE classifications ADD COLUMN spam_verdict TEXT NOT NULL DEFAULT 'not_spam' CHECK(spam_verdict IN ('spam','not_spam','uncertain'));
ALTER TABLE classifications ADD COLUMN action_required INTEGER NOT NULL DEFAULT 0 CHECK(action_required IN (0,1));
ALTER TABLE classifications ADD COLUMN action_type TEXT NOT NULL DEFAULT 'none';
ALTER TABLE classifications ADD COLUMN summary TEXT NOT NULL DEFAULT '';
ALTER TABLE classifications ADD COLUMN manual_override INTEGER NOT NULL DEFAULT 0 CHECK(manual_override IN (0,1));
ALTER TABLE classifications ADD COLUMN reviewed_by INTEGER;
ALTER TABLE classifications ADD COLUMN reviewed_at TEXT;
UPDATE classifications SET spam_verdict=CASE WHEN is_spam=1 THEN 'spam' ELSE 'not_spam' END;
UPDATE classifications SET needs_review=1 WHERE importance='uncertain' OR category='uncertain';
CREATE INDEX IF NOT EXISTS idx_classification_filter ON classifications(category, importance, spam_verdict, include_in_report);

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
