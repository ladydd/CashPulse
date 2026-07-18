# Security

## Report issues

Please open a GitHub issue for non-sensitive bugs. For vulnerabilities that could
expose credentials or private financial data, contact the repository owner
privately when possible.

## Deploy checklist

- Generate long random values for `ADMIN_PASSWORD` and `INGEST_TOKEN`.
- Never commit `.env` or SQLite data files.
- Prefer HTTPS and `SECURE_COOKIE=true` in production.
- Put **Ingest Token only** in the iPhone Shortcut header:
  - `Authorization: Bearer <INGEST_TOKEN>`
  - or `X-API-Token: <INGEST_TOKEN>`
- Do **not** put tokens in URL query strings (`?token=`).
- Web UI is **password + session cookie** only. `ADMIN_TOKEN` is optional for scripts.
- Login is rate-limited / lockout-protected against brute force; keep the password strong anyway.

## What this project does not do

- CashPulse does not call your bank. It only receives SMS text you forward.
- SMS content and balances stay on the machine you deploy to.
