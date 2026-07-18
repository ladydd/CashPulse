# CashPulse

银行短信流水收集与消费分析。iPhone 快捷指令转发完整短信 → Go API 解析落库 → Web 看板查看。

## 功能

- `POST /api/v1/sms` 接收完整短信原文（`INGEST_TOKEN`）
- 邮储等短信解析：金额 / 收支 / 渠道归一 / 类型（消费·转账·理财…）/ 余额 / 时间
- 原始短信始终落库；解析失败可查看
- **今日首页**：账户余额、当月收支、预算余量、消费节奏、最近流水
- **分析**：按月（主）/ 7·15·30·90 天 / 全部；消费与转账拆分；谁花了多少
- **整理**：按日打归属人与标签；支持自动打标规则
- 预算、存钱目标、CSV 导出、银行卡汇总
- 鉴权：网页 **仅管理员密码**（Session 持久化到 SQLite，约 30 天）；短信上报用独立 `INGEST_TOKEN`；`ADMIN_TOKEN` 仅可选给脚本
- 响应式 Web（React）：桌面侧栏 + 移动底栏

## 技术栈

| 层 | 选型 |
|----|------|
| 后端 | Go 1.22+、标准库 `net/http` |
| 存储 | SQLite（`modernc.org/sqlite`） |
| 前端 | Vite + React + Chart.js（产物 embed 进二进制） |

## 快速开始

```bash
# 前端
cd web && npm install && npm run build && cd ..
mkdir -p cmd/server/dist && cp -R web/dist/. cmd/server/dist/

# 后端
go build -o bin/cashpulse ./cmd/server

# 运行（开发示例）
API_TOKEN=dev-token \
INGEST_TOKEN=dev-token \
ADMIN_PASSWORD=admin-pass \
PORT=8080 \
DATABASE_PATH=./data/cashpulse.db \
./bin/cashpulse
```

浏览器打开 http://127.0.0.1:8080 ，密码 `admin-pass` 或 Token `dev-token`。

导入历史短信 CSV（只用 `text` 列）：

```bash
go run ./cmd/import -file tmp/xxx.csv -db ./data/cashpulse.db -reset
```

## 配置

见 `.env.example` 与 `deploy/`。

| 变量 | 含义 |
|------|------|
| `INGEST_TOKEN` | 仅短信上报 |
| `ADMIN_PASSWORD` | **必填**，浏览器登录 |
| `INGEST_TOKEN` | 短信上报专用 |
| `ADMIN_TOKEN` | 可选，脚本调管理 API |
| `DATABASE_PATH` | SQLite 路径 |
| `SECURE_COOKIE` | HTTPS 下设 `true` |

## 部署

见 `deploy/README.md`（systemd + Caddy 示例）。

## 说明

- 个人项目；流水数据与短信导出请勿提交仓库（已在 `.gitignore`）。
- 私有备份：GitHub `ladydd/CashPulse`。
