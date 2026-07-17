# CashPulse 部署说明

## 鉴权模型

| 调用方 | 凭证 | 权限 |
|--------|------|------|
| iPhone 快捷指令 | `INGEST_TOKEN`（Bearer / X-API-Token） | 仅 `POST /api/v1/sms` |
| 浏览器管理 | `ADMIN_PASSWORD` 登录 → Session Cookie | 全部管理 API + 页面 |
| 可选 | `ADMIN_TOKEN` Bearer | 同管理权限（脚本用） |

兼容：只设 `API_TOKEN` 时，写入与管理共用该值（不推荐公网）。

## 推荐目录

```text
/opt/cashpulse/cashpulse          # 二进制
/etc/cashpulse.env                # 密钥 (600)
/var/lib/cashpulse/cashpulse.db   # SQLite
```

## 步骤摘要

1. 构建：`make build` 或 `go build -o cashpulse ./cmd/server`（先 `cd web && npm i && npm run build` 并 sync dist）
2. 上传二进制到服务器 `/opt/cashpulse/`
3. 配置 `/etc/cashpulse.env`（参考 `env.example`）
4. 安装 systemd unit：`deploy/cashpulse.service`
5. 反代 HTTPS（Caddy / Nginx / Cloudflare Tunnel）
6. 快捷指令：`POST https://你的域名/api/v1/sms` + `Authorization: Bearer <INGEST_TOKEN>`
7. 浏览器打开域名 → 用 `ADMIN_PASSWORD` 登录

## 备份

```bash
sqlite3 /var/lib/cashpulse/cashpulse.db ".backup '/var/backups/cashpulse-$(date +%F).db'"
```
