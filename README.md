# CashPulse

**用 iPhone 快捷指令，把银行短信变成自己的账本。**

收到银行短信 → 快捷指令自动转发全文 → CashPulse 解析入库 → 手机/电脑打开看板看消费。  
数据只存在你自己的服务器上，不经过第三方记账 App。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react)](https://react.dev/)

## 它解决什么问题

很多人记账半途而废，是因为**每笔都要手抄**。  
CashPulse 的路径是：

1. iPhone 收到银行短信（如邮储 95580）  
2. **快捷指令**在「短信」条件下自动 `POST` 完整原文  
3. 服务端解析金额、时间、商户、余额、消费/转账类型  
4. 打开 Web 看本月花了多少、谁花的、预算还剩多少  

适合：**自托管、隐私优先、愿意自己搭一台小服务** 的个人/家庭。

## 界面预览

### 今日（桌面）

![今日首页](docs/screenshots/01-home.png)

余额、本月收支、预算余量、按金额的消费节奏、最近流水。  
**月份可左右切换**（按月看账）。

### 分析

![消费分析](docs/screenshots/02-analysis.png)

近 7/15/30/90 天或按月；**仅消费 / 仅转账** 拆分；渠道构成；「谁花了多少」。

### 流水

![流水列表](docs/screenshots/03-transactions.png)

按日分组的流水，支持搜索与加载更多。

### 整理（打标）

![整理流水](docs/screenshots/04-organize.png)

按今天/昨天/近几天，给流水点「我 / 老婆 / 孩子」和标签。

### 手机

![手机首页](docs/screenshots/05-mobile-home.png)

底部导航，适配手机浏览与整理。

## 和 iPhone 快捷指令怎么接

```text
┌─────────────┐     自动转发全文      ┌──────────────────┐
│ 银行短信     │ ──────────────────► │ 快捷指令          │
│ (如 95580)  │                      │ 当收到短信时…     │
└─────────────┘                      └────────┬─────────┘
                                              │ HTTPS POST
                                              │ Authorization: Bearer <INGEST_TOKEN>
                                              ▼
                                     ┌──────────────────┐
                                     │ CashPulse 服务    │
                                     │ 解析 → SQLite     │
                                     └────────┬─────────┘
                                              │
                         ┌────────────────────┼────────────────────┐
                         ▼                    ▼                    ▼
                      今日看板              分析报表              家庭打标
```

### 快捷指令配置要点

| 项 | 值 |
|----|-----|
| 触发 | 收到短信（可筛选发件人/关键词，如 95580、邮储） |
| 动作 | **获取 URL 内容**（或「URL」+ POST） |
| URL | `https://你的域名/api/v1/sms` |
| 方法 | `POST` |
| 请求头 | `Authorization` = `Bearer <你的 INGEST_TOKEN>` |
| 请求头 | `Content-Type` = `application/json` |
| 请求体 | JSON：`{"text":"短信全文","source":"iphone"}` |

`text` 必须是**完整短信原文**（含【邮储银行】…），不要只截金额。  
Token 放在 **Header**，不要拼在 URL `?token=` 里。

相同短信正文重复提交会去重（重试安全），不会重复计账。

## 功能一览

- 短信入库与幂等（防快捷指令重试重复记账）
- 邮储短信解析：金额 / 收支 / 商户归一 / 类型（消费·转账·理财…）/ 余额 / 时间
- 今日：余额、当月收支、预算、消费节奏（柱高=金额）、最近流水
- 分析：按月为主，支持 7/15/30/90 天；消费与转账分开；谁花了多少
- 整理：按日打归属人与标签；入库自动规则
- 预算、存钱目标、CSV 导出、银行卡汇总
- 网页：**管理员密码**登录（Session 约 30 天，防爆破锁定）
- 短信上报：**独立 INGEST_TOKEN**（与网页密码分开）
- 单二进制 + 内嵌前端；SQLite；可 Docker / Caddy 部署

## 技术栈

| 层 | 选型 |
|----|------|
| API | Go 1.22+、标准库 `net/http` |
| 存储 | SQLite（`modernc.org/sqlite`） |
| 前端 | Vite + React + Chart.js（构建后 embed 进二进制） |

## 快速开始

```bash
# 前端
cd web && npm install && npm run build && cd ..
mkdir -p cmd/server/dist && cp -R web/dist/. cmd/server/dist/

# 后端
go build -o bin/cashpulse ./cmd/server

# 运行（开发示例，务必改成自己的密钥）
export INGEST_TOKEN=dev-ingest
export ADMIN_PASSWORD=dev-password
export PORT=8080
export DATABASE_PATH=./data/cashpulse.db
export TZ_NAME=Asia/Shanghai
./bin/cashpulse
```

浏览器打开 http://127.0.0.1:8080 ，用 `ADMIN_PASSWORD` 登录。

导入历史短信（CSV 只用 **`text` 列**）：

```bash
go run ./cmd/import -file path/to/export.csv -db ./data/cashpulse.db
```

## 配置

详见 `.env.example` 与 [`deploy/`](deploy/)。

| 变量 | 必填 | 用途 |
|------|------|------|
| `ADMIN_PASSWORD` | **是** | 网页登录密码 |
| `INGEST_TOKEN` | **是** | 仅短信上传（给快捷指令） |
| `ADMIN_TOKEN` | 否 | 可选，脚本调管理 API（网页不用） |
| `DATABASE_PATH` | 否 | SQLite 路径 |
| `PORT` | 否 | 监听端口，默认 `8080` |
| `TZ_NAME` | 否 | 解析/统计/CSV 时区，默认 `Asia/Shanghai` |
| `SECURE_COOKIE` | 否 | 公网 HTTPS 请设 `true` |
| `SESSION_TTL_HOURS` | 否 | 登录保持小时数，默认约 720（30 天） |

兼容：若未设 `INGEST_TOKEN`，可回退使用 `API_TOKEN`。

## 短信接口

```http
POST /api/v1/sms
Authorization: Bearer <INGEST_TOKEN>
Content-Type: application/json

{"text":"【邮储银行】…完整短信…","source":"iphone"}
```

成功：`201`；已处理过的相同正文：`200` 且 `"duplicate": true`，并带回原交易。

## 部署

见 [`deploy/README.md`](deploy/README.md)（Docker、Caddy、systemd 示例）。

开源的是**代码**；你的流水库和密钥只在你自己机器/服务器上，不会随 `git clone` 带走。

## 安全

见 [SECURITY.md](SECURITY.md)。

- 网页密码与短信 Token **分开**
- 登录有防爆破（错误过多会暂时锁定新登录；**已登录会话不受影响**）
- 请勿把真实短信导出、`.env`、数据库提交进 Git

## 参与贡献

欢迎 Issue / PR：更多银行短信模板、解析规则、界面改进。  
请勿提交真实短信原文或生产密钥。

## License

[MIT](LICENSE)
