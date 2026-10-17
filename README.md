# Mail Manager — local AI-powered email classification

Go + Gin + SQLite + Yandex IMAP + local llama.cpp / Qwen. Classification is read-only; a separately authenticated human administrator may manually move selected messages to Trash. The LLM never gets mailbox credentials or mutation tools.

## Upgrade / first run

Stop any running instance. Backup the existing SQLite database **before** applying migration v4:

```sh
sqlite3 data/mail-manager.db ".backup 'data/mail-manager.backup.db'"
```

Copy `.env.example` to `.env` only for a new setup. **On an existing installation keep your current `.env` and database**, then add the new settings. The backend stores embedded SQL migrations (v1–v4).

```sh
cp .env.example .env
chmod 600 .env
openssl rand -hex 32 # assign result to JWT_SECRET
```

Configure IMAP (OAuth `mail:imap_ro` for read-only), `LLM_BASE_URL`, `LLM_MODEL` and `JWT_SECRET`. For first administrator (if no users exist):

```sh
# In fish shell
read -s -P 'New admin password: ' ADMIN_PASS
printf '%s\n' "$ADMIN_PASS" | go run ./cmd/admin -username admin
set -e ADMIN_PASS
```

Compile and run (Go version per go.mod):

```sh
go mod tidy
go test ./...
go vet ./...
go run ./cmd/server
```

## Manual deletion — disabled by default

1. Obtain Yandex OAuth scope `mail:imap_full` when you are ready for human-controlled deletion; `mail:imap_ro` cannot move messages.
2. Call `GET /api/mail/folders` with admin JWT to learn the exact Trash folder name. Use the folder with the `\\Trash` attribute where possible.
3. In `.env`, set `MAIL_TRASH_FOLDER=<exact name>` and `MAIL_ALLOW_DELETE=true`, then restart.
4. The React frontend should call `/api/mail/delete/preview` to display the selected sender/subject, then require a second explicit confirmation before calling `POST /api/mail/delete` with `{ "ids": [1,2], "confirm": "MOVE_TO_TRASH", "password": "<admin password>" }`.

The API checks roles, current UIDVALIDITY and configured account/folder; it does not offer an expunge or send operation. MOVE failures may be ambiguous; check `/api/queue/problems` and audit rather than automatically repeating deletion.

**Never publish `.env`, database files, `.git` metadata with secret history, or OAuth tokens.** The distributed source archive intentionally excludes all of these.

## Frontend API

See [API.md](API.md). React can use Vite's `/api` proxy in development or a same-origin HTTPS proxy for deployment. All mail/report API calls use Bearer JWT; admin operations are role-restricted. The system keeps message bodies out of SQLite; React can request individual bodies via a read-only endpoint.

## Testing and caveats

Run `go test ./...`, `go vet ./...` and test a staged copy of your database. Integration with real Yandex IMAP and local Qwen remains necessary. Expect occasional duplicate *LLM requests* after a crash; the mailbox UID unique constraint prevents duplicate local email rows. Background sync depends on a valid OAuth token; refresh is not implemented.

If a MOVE is inconclusive, the backend marks the message for manual reconciliation; see `API.md`. Human confirmation is password-protected and an audit record is retained.

React TypeScript types are in `contracts/mail-manager.d.ts`.
