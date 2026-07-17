# CashPulse

银行短信流水收集与每日消费汇总。iPhone 快捷指令转发完整短信 → Go API 解析落库 → Web 看板查看。

## 功能（MVP）

- `POST /api/v1/sms` 接收完整短信原文
- 规则解析金额 / 收支 / 商户 / 卡尾号 / 时间（可按银行扩展）
- 原始短信始终落库；解析失败可在 Web「未解析」中查看
- 今日支出 / 近 7 日柱状汇总 / 流水列表
- 单用户 Token 鉴权，Go + SQLite，内存占用低

## 技术栈

| 层 | 选型 |
|----|------|
| 后端 | Go 1.22+、标准库 `net/http` |
| 存储 | SQLite（`modernc.org/sqlite`，纯 Go） |
| 前端 | Vite + 原生 JS（前后端分离，产物 embed 进二进制） |

## 快速开始

### 1. 配置

```bash
cp .env.example .env
# 编辑 API_TOKEN 为一个足够长的随机串
```

或直接用环境变量（无需 dotenv 文件，进程启动时传入即可）。

### 2. 一键构建并运行

需要：Go 1.22+、Node.js 18+。

```bash
make run API_TOKEN='your-secret-token'
```

浏览器打开：http://127.0.0.1:8080  
在页面中填入同一个 Token。

### 3. 开发模式（前后端热更新）

终端 1 — API：

```bash
make dev-api API_TOKEN='dev-token'
```

终端 2 — 前端：

```bash
cd web && npm install && npm run dev
```

打开 Vite 地址（默认 http://127.0.0.1:5173），API 经代理转发到 8080。

## API

除 `GET /api/v1/health` 外均需鉴权，任选其一：

- Header：`Authorization: Bearer <token>`
- Header：`X-API-Token: <token>`
- Query：`?token=<token>`（快捷指令不方便加 Header 时用）

### 接收短信

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/sms \
  -H "Authorization: Bearer your-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"text":"【工商银行】您尾号1234卡07月16日12:30在星巴克消费100.50元，余额2000.00元。","source":"iphone"}'
```

也支持 `text/plain` 直接 POST 短信正文。

### 其他

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/health` | 健康检查（无需 Token） |
| GET | `/api/v1/dashboard?days=7` | 今日 + 近 N 日汇总 |
| GET | `/api/v1/transactions?q=&limit=50` | 流水列表 |
| GET | `/api/v1/unparsed` | 未解析短信 |

## iPhone 快捷指令（示意）

1. 自动化 → 当收到信息（可限定发件人/关键词为银行）
2. 获取短信内容
3. **获取 URL 内容**：
   - URL：`https://你的域名/api/v1/sms`
   - 方法：POST
   - 请求头：`Authorization` = `Bearer <token>`，`Content-Type` = `application/json`
   - 请求体：JSON `{"text": "<短信内容>", "source": "iphone"}`

公网访问需自行用反向代理 / 内网穿透（如 Cloudflare Tunnel、frp、Tailscale）。**不要把 Token 写进公开仓库。**

## 项目结构

```
CashPulse/
├── cmd/server/          # 入口 + embed 前端 dist
├── internal/
│   ├── api/             # HTTP
│   ├── config/
│   ├── model/
│   ├── parser/          # 短信解析（可加银行规则）
│   ├── service/
│   └── store/           # SQLite
├── web/                 # Vite 前端源码
├── Makefile
└── README.md
```

## 解析规则

- **邮储银行（主卡）**：按真实 95580 短信模板解析（支出/收入金额、余额、尾号、`YY年MM月DD日HH:MM` 时间、渠道如微信/支付宝/拼多多等）。验证码短信自动忽略。
- 另有通用中文银行兜底规则。

### 导入历史导出

```bash
make import-sms FILE=tmp/95580短信完整导出.txt
# 或
go run ./cmd/import -file tmp/95580短信完整导出.txt -db ./data/cashpulse.db
```

导入后启动服务即可在 Web 看到历史流水与最新余额。

## 许可

私人项目，按需使用。
