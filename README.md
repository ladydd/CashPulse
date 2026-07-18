# CashPulse

Self-hosted personal finance from **bank SMS**.

iPhone Shortcut (or any client) posts the full SMS body → Go API parses & stores → React dashboard for spend analysis, budgets, and household labeling.

**License:** MIT

## Features

- `POST /api/v1/sms` ingest of full SMS text (`INGEST_TOKEN`)
- PSBC (邮储) parsers: amount, direction, merchant normalization, kind (consume / transfer / invest / …), balance, time
- Raw SMS always stored; parse failures retained
- Home: balance, month in/out, budget remaining, daily spend (by **amount**), recent activity
- Analysis: month-first ranges, spend vs transfer, “who spent how much”
- Labeling: person + tags, auto rules on ingest
- Budgets, savings goals, CSV export, card summary
- Auth: **web = admin password + session** (SQLite-backed, ~30 days sliding); SMS uses separate ingest token
- Responsive React UI (desktop sidebar + mobile bottom nav)
- Single binary with embedded UI; SQLite storage

## Stack

| Layer | Choice |
|-------|--------|
| API | Go 1.22+, `net/http` |
| DB | SQLite (`modernc.org/sqlite`) |
| Web | Vite + React + Chart.js (embedded in binary) |

## Quick start

```bash
# Web
cd web && npm install && npm run build && cd ..
mkdir -p cmd/server/dist && cp -R web/dist/. cmd/server/dist/

# Server
go build -o bin/cashpulse ./cmd/server

# Run (dev example — change secrets)
export INGEST_TOKEN=dev-ingest
export ADMIN_PASSWORD=dev-password
export PORT=8080
export DATABASE_PATH=./data/cashpulse.db
export TZ_NAME=Asia/Shanghai
./bin/cashpulse
```

Open http://127.0.0.1:8080 and sign in with `ADMIN_PASSWORD`.

Import historical SMS export (uses **text** column only for CSV):

```bash
go run ./cmd/import -file path/to/export.csv -db ./data/cashpulse.db
```

## Configuration

See `.env.example` and `deploy/`.

| Variable | Required | Purpose |
|----------|----------|---------|
| `ADMIN_PASSWORD` | **yes** | Web login password |
| `INGEST_TOKEN` | **yes** | SMS upload only |
| `ADMIN_TOKEN` | no | Optional Bearer for scripts (not the web UI) |
| `DATABASE_PATH` | no | SQLite path (default `./data/cashpulse.db`) |
| `PORT` | no | Listen port (default `8080`) |
| `TZ_NAME` | no | Timezone for parse/stats/CSV (default `Asia/Shanghai`) |
| `SECURE_COOKIE` | no | `true` behind HTTPS |
| `SESSION_TTL_HOURS` | no | Session lifetime hours (default ~720) |

`API_TOKEN` still works as a legacy fallback for `INGEST_TOKEN` if ingest is unset.

## SMS upload

```http
POST /api/v1/sms
Authorization: Bearer <INGEST_TOKEN>
Content-Type: application/json

{"text":"【邮储银行】…full SMS…","source":"iphone"}
```

Identical SMS bodies are deduplicated (SHA-256 of normalized text). Concurrent retries return the same transaction with `"duplicate": true`.

## Deploy

See [`deploy/README.md`](deploy/README.md) for Docker / Caddy / systemd examples.

Your **production secrets and SQLite data live only on your server**, not in this repository. Cloning the public repo does not expose anyone else’s ledgers.

## Local development vs open source

- This GitHub tree is the shared **source code**.
- Local `./data/`, `./tmp/`, and `.env` are gitignored — keep your real SMS and passwords there.
- Deployed instances (your own domain, tokens, DB) are independent of other forks/clones.

## Security

See [SECURITY.md](SECURITY.md).

## Contributing

Issues and PRs welcome: more bank SMS patterns, parsers, and UI polish. Please do not commit real SMS dumps or production credentials.
