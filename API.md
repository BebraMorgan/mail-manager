# Mail Manager API (development, single administrator)

All endpoints use `http://127.0.0.1:3000` behind the Vite proxy or a same-origin HTTPS reverse proxy. No bearer tokens and no user-management API exist. The server supports exactly one administrator provisioned by `go run ./cmd/admin -username admin`, with an interactive password supplied on stdin.

## Authentication

- `POST /api/auth/login` `{ "username": "admin", "password": "..." }` — sets `mm_session` (HttpOnly access JWT), `mm_refresh` (HttpOnly opaque refresh), `mm_csrf` (readable CSRF cookie); returns `{ "status": "ok" }`.
- `GET /api/auth/me` — administrator public profile, access cookie required.
- `POST /api/auth/refresh` — rotates refresh cookie and returns `{ "status": "ok" }`. No JSON refresh token. Requires `X-CSRF-Token: <mm_csrf cookie>`; must run without a valid access cookie after JWT expiry.
- `POST /api/auth/logout` — revokes refresh session and clears cookies; requires CSRF header.
- `POST /api/auth/password` `{ "current_password": "...", "new_password": "..." }` — invalidates JWT versions and all refresh sessions; requires valid access and CSRF header.

Cookie authentication requires a same-origin browser setup. Axios: `withCredentials: true`; on non-GET requests add the readable `mm_csrf` value to `X-CSRF-Token`. If a protected request returns 401, call `/api/auth/refresh` once (single-flight across concurrent requests), then retry the protected request; otherwise clear frontend session and show login. Do not send refresh tokens as JSON. The client must not store JWT in localStorage.

## Mail account via API, not environment

- `GET /api/mail/settings` — returns nonsecret settings or `{ "configured": false }`.
- `POST /api/mail/settings` — sets the active Yandex IMAP mailbox, stored encrypted in SQLite. POST JSON example:
```json
{
 "host": "imap.yandex.ru", "port": 993, "username": "example@yandex.ru", "auth_method": "oauth2",
 "oauth_token": "TOKEN", "folder": "INBOX", "trash_folder": "Trash", "allow_delete": false,
 "poll_interval_seconds": 300, "fetch_limit": 30, "max_message_bytes": 1048576
}
```
For app password use `"auth_method": "app_password"` and `"app_password": "..."`. GET never returns credentials. Empty credential in a POST preserves the existing credential when host, username, and authentication method match. Only `imap.yandex.ru` and `imap.yandex.com` are allowed as hosts. Changing the configured account or folder when emails already exist requires resetting the development database.

- `POST /api/mail/sync` — trigger scan.
- `GET /api/mail/folders` — IMAP folder list.
- `GET /api/mail/:id/content` — read body (BODY.PEEK).
- `POST /api/mail/delete/preview` `{ "ids": [1,2] }` — preview.
- `POST /api/mail/delete` `{ "ids": [1,2], "confirm": "MOVE_TO_TRASH", "password": "..." }` — **human-triggered** MOVE to trash. OAuth must have IMAP write permissions, and `allow_delete` must be true. The model cannot call this route. No permanent expunge endpoint exists.
- `GET /api/mail/delete-audit` — history.
- `POST /api/mail/delete/:id/reconcile` — manual reconciliation after ambiguous MOVE.

## Classification and reports

- `GET /api/classifications`, `GET /api/classifications/:id`, `GET /api/classification/schema`.
- `PATCH /api/classifications/:id` — manual classification override.
- `POST /api/classifications/:id/retry` — manual retry.
- `GET /api/queue/stats`, `GET /api/queue/problems`, `GET /api/reports/daily?date=YYYY-MM-DD`.
- `GET /health` — public health check, `GET /api/health/db` — protected database check.

No legacy `GET /api/reports/items`, `/api/users` endpoints, Bearer-auth fallback, or historical migration chain.

## React production SPA

If `FRONTEND_DIST` points to a built Vite `dist/` directory, Gin serves the
React application on the same port as the API. `GET`/`HEAD` client routes
fall back to `index.html`; API routes and missing static files do not.
See README.md for build/deployment instructions.
