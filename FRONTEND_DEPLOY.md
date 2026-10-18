# React build served by Mail Manager (Gin)

The patch is based on the `mail-manager-single-admin.zip` backend.

## Applying the patch

Copy the patch's contents into the backend project root. Existing `.env`, `data/` and `.git` are not included in this patch and must not be overwritten.

## Build and deploy on CachyOS (Fish)

```fish
cd ~/Projects/mail-manager-react
npm ci
npm run build

mkdir -p ~/mail-manager/web/dist
cp -a dist/. ~/mail-manager/web/dist/
```

Add to the existing `~/mail-manager/.env`:

```dotenv
FRONTEND_DIST=./web/dist
```

The backend must be started with `~/mail-manager` as the working directory:

```fish
cd ~/mail-manager
go test ./internal/frontend
go build -o bin/mail-manager ./cmd/server
./bin/mail-manager
```

Open `http://127.0.0.1:3000` in a browser. Test page refresh on `/mail`, `/reports`, `/settings`, and that API JSON still works.

```fish
curl -I http://127.0.0.1:3000/
curl -I http://127.0.0.1:3000/mail
curl -i http://127.0.0.1:3000/api/missing
curl -i http://127.0.0.1:3000/assets/missing.js
```

Expect HTML on `/` and `/mail`; JSON 404 for unknown `/api/*`; 404 for missing JS and CSS.

## Important

- Never put `.env`, databases, or other secrets inside the public `web/dist` directory.
- Do not push dist to Git; it's a generated artifact.
- `FRONTEND_DIST` is resolved from the current working directory; specify an absolute path if you run Gin from elsewhere or via systemd.
- If React and API are served from this origin, no CORS configuration is needed. HttpOnly and CSRF cookies continue to work through `/api`.
- In production, keep Gin on loopback and use an HTTPS reverse proxy. `APP_ENV=production` sets the cookie `Secure` flag.
- No `go:embed` is used: you can update React by rebuilding and recopying `dist` without recompiling Go.
- `FRONTEND_DIST` is optional; omitting it leaves the server API-only.
- This patch has not been compiled in the available environment because it runs Go 1.23.2 while this backend requires Go 1.27.1. Run Go tests and the build on the deployment machine.
