# Mail Manager backend

Single-user development backend for Gin, SQLite, Yandex IMAP, Qwen3 via llama.cpp, and a React frontend.

## Clean installation

**Destructive database reset:** stop the server; back up the previous `data/` directory if needed and remove the old SQLite database including `-wal` and `-shm` sidecars. The only supported schema starts from an empty database (`internal/storage/migrations/001_schema.sql`). There is no migration compatibility with older versions.

Copy `.env.example` to `.env`, generate two **different** random values using `openssl rand -hex 32`: one for `JWT_SECRET`, one for `MAIL_SETTINGS_KEY`. Keep them secret and do not commit `.env`. `MAIL_SETTINGS_KEY` encrypts stored IMAP credentials with AES-256-GCM; losing it makes existing encrypted settings unreadable.

Run `go mod download`, `go test ./...`, `go vet ./...`, `go run ./cmd/admin -username admin` (enter admin password on stdin), then `go run ./cmd/server`. Go version is specified in go.mod. Only one administrator can be created (`id=1`); no role or user endpoints remain.

The server runs on localhost:3000, Qwen at localhost:8080. Configure IMAP **only by POST /api/mail/settings** after login, never from `.env`. Until configured, IMAP background processing is idle. Credentials are never returned by GET and are encrypted in SQLite. All mail operations require the administrator's session; moving emails to trash additionally requires password re-entry.

## JWT cookie authentication

`mm_session`: HttpOnly JWT access cookie, 15 minutes by default. `mm_refresh`: rotating, one-use HttpOnly refresh cookie, 30 days. `mm_csrf`: double-submit CSRF cookie. `POST /api/auth/refresh` exchanges the refresh cookie for new cookies without exposing JWT to JavaScript; `POST /api/auth/logout` revokes it. Use same-origin deployment (Vite proxy in dev / HTTPS reverse proxy in production); send `X-CSRF-Token` for mutations. Do not store tokens in localStorage.

See [API.md](API.md) for exact routes and JSON shapes. This archive deliberately excludes `.env`, `.git`, databases, and frontend changes.

## Serve the React production build from Gin (one origin)

The frontend and API can run on the same `http://127.0.0.1:3000` origin;
JWT and CSRF cookies work without CORS or a Vite proxy. In a production
installation, put an HTTPS reverse proxy in front of Gin and keep APP_HOST
on loopback.

Build React and copy the **contents** of its `dist/` directory into a public
build directory owned by the Go deployment. From a Fish shell:

```fish
cd ~/Projects/mail-manager-react
npm ci
npm run build
mkdir -p ~/mail-manager/web/dist
cp -a dist/. ~/mail-manager/web/dist/
```

Set `FRONTEND_DIST=./web/dist` in the backend `.env` (or use an absolute path),
then run `go run ./cmd/server` or your compiled Go binary from the backend
directory. If `FRONTEND_DIST` is omitted, Gin runs API-only. If configured but
missing `index.html`, startup fails with a clear error.

- `GET /`, `/login`, `/mail`, `/reports`, `/settings`, etc. serve the SPA.
- `/assets/*` and public Vite files are served from the built directory.
- Unknown `/api/*` paths return JSON 404, not React HTML.
- Missing JS/CSS return 404, not React HTML.
- React `index.html` uses `Cache-Control: no-store`; Vite hashed assets are cached immutably.
- `FRONTEND_DIST` is resolved relative to the **server's working directory**;
  use an absolute path if running via systemd without `WorkingDirectory`.

`dist/` contains public website files only. Never place `.env`, database files,
OAuth tokens, private keys or other server secrets into it. Rebuild React and
recopy dist when changing the UI; the Go server serves the updated files.
