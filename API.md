# Mail Manager API (v2 frontend contract)

All `/api/*` endpoints except `/api/auth/login` require `Authorization: Bearer <JWT>`. The backend binds to `127.0.0.1` only; use a TLS reverse proxy/VPN for remote use. `admin` is required for modifying any stored classification or the mailbox.

## Authentication and users

- `POST /api/auth/login` — `{"username":"admin","password":"..."}` → access_token, token_type.
- `GET /api/auth/me` and `POST /api/auth/password` — authenticated.
- `GET /api/users`, `POST /api/users`, `DELETE /api/users/:id` — admin.

## Classifications

- `GET /api/classification/schema` — stable taxonomy and permitted enum values.
- `GET /api/classifications?limit=50&offset=0&category=work&importance=high&spam_verdict=not_spam&include_in_report=true&needs_review=false&q=subject` — paginated response `{items,total,count,limit,offset}`. Every filter is optional, maximum limit 200.
- `GET /api/classifications/:id` — metadata and classification details.
- `GET /api/mail/:id/content` — on-demand read-only IMAP fetch; JSON with sender, subject, body; never cached or persisted by backend.
- `PATCH /api/classifications/:id` — **admin only**, human review, full object. Example:

```json
{
  "spam_verdict":"not_spam",
  "category":"work",
  "importance":"high",
  "action_required":true,
  "action_type":"reply",
  "summary":"Коллега просит ответить на вопрос.",
  "reason":"Есть конкретный запрос, требующий ответа.",
  "include_in_report":true
}
```

- `POST /api/classifications/:id/retry` — **admin only**, explicitly schedules fresh LLM classification. Resets a human override; use only deliberately. LLM has no mailbox mutation capability.

Taxonomy:

| Field | Values |
|---|---|
| spam_verdict | `spam`, `not_spam`, `uncertain` |
| category | `personal`, `work`, `finance`, `security`, `shopping`, `delivery`, `account`, `social`, `newsletter`, `promotion`, `notification`, `other`, `uncertain` |
| importance | `low`, `medium`, `high`, `uncertain` |
| action_type | `none`, `reply`, `review`, `payment`, `deadline`, `security`, `other` |

`include_in_report` is computed by Go for LLM-generated classification; a human can override it. `needs_review` is true for uncertain/truncated content. No classification ever triggers an IMAP delete.

## Reports and queue

- `GET /api/queue/stats`
- `GET /api/queue/problems?limit=50` — includes failed/skipped, uncertain, and unconfirmed mailbox moves.
- `GET /api/reports/daily?date=YYYY-MM-DD` — UTC day, excludes messages moved to trash; maximum 1000 items.
- `GET /api/reports/items` — legacy compact report items.
- `POST /api/mail/sync` — admin, manual scan (classification remains asynchronous).

## Human-only multi-delete (move to Trash)

1. `GET /api/mail/folders` (**admin**) — inspect server folder names and `\\Trash` special-use attribute; configure `.env` accordingly.
2. `POST /api/mail/delete/preview` (**admin**) — `{"ids":[12,13]}` → metadata for review/confirmation modal, no changes.
3. `POST /api/mail/delete` (**admin**) — `{"ids":[12,13],"confirm":"MOVE_TO_TRASH","password":"<admin password>"}`. Maximum 100 IDs. Returns per-ID statuses and `207 Multi-Status` for partial failure.
4. `GET /api/mail/delete-audit` (**admin**) — recent audit trail.
5. `POST /api/mail/delete/:id/reconcile` (**admin**) — only after a human verifies the Yandex mailbox, `{ "resolution":"in_inbox"|"moved_to_trash", "password":"<admin password>" }`, for a message in `unknown` state. No IMAP mutation occurs in this endpoint.

Deletion requires **re-entering the administrator password**; JWT alone is not sufficient. `MAIL_ALLOW_DELETE` defaults to false. Deletion becomes available only when an administrator deliberately enables it and supplies `MAIL_TRASH_FOLDER` (different from INBOX). On Yandex OAuth grant `mail:imap_full` (read and delete), **not** SMTP permissions. The server must advertise IMAP MOVE; automatic fallback to unscoped EXPUNGE is refused. No permanent expunge endpoint is provided. The requested email IDs refer to local SQLite IDs, *not* raw IMAP UIDs.

On an ambiguous IMAP/network failure, the email is marked `unknown` and appears in `/api/queue/problems`; the backend deliberately refuses automatic retries of the move, because it may have already succeeded remotely. A human must reconcile the state with the mailbox. Emails marked deleted are retained in SQLite for audit (with `deleted_at`, `deleted_by`) and excluded from reports.

## Operational caveats

- No automatic OAuth refresh in this build.
- IMAP library operations still may block on an unresponsive server after a connection (dial is time-bounded); do not expose APIs to untrusted networks.
- These are *contract definitions*; integration-test against your Yandex account and llama.cpp before deploying.

TypeScript consumers can copy the checked-in `contracts/mail-manager.d.ts` interfaces.
