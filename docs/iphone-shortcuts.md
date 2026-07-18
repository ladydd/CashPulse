# iPhone 快捷指令接入教程

本教程用于把 iPhone 收到的银行短信自动发送到 CashPulse。

完成后，数据链路如下：

```text
银行短信
   ↓
iPhone 快捷指令自动化
   ↓  HTTPS POST
CashPulse /api/v1/sms
   ↓
解析并写入账本
```

> 本教程以邮储银行短信号码 `95580` 为例。其他银行只需要替换发送号码。

## 准备工作

开始前请确认：

- CashPulse 已经部署并可通过 HTTPS 访问
- 你知道 CashPulse 的 `INGEST_TOKEN`
- 接口地址格式为：`https://你的域名/api/v1/sms`
- iPhone 已安装系统自带的「快捷指令」App

建议先在电脑上用 `curl` 验证接口可用：

```bash
curl -X POST 'https://你的域名/api/v1/sms' \
  -H 'Authorization: Bearer 你的_INGEST_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"text":"【测试银行】尾号1234账户消费1.00元","source":"curl-test"}'
```

接口正常时会返回成功结果，并在 CashPulse 中生成一条测试记录。

## 第一步：创建“收到信息”自动化

在 iPhone 上打开：

```text
快捷指令 → 自动化 → 右上角「+」
```

选择「信息」触发器，然后配置：

| 项目 | 示例 |
|---|---|
| 发件人 | `95580` |
| 信息包含 | 留空 |
| 运行方式 | **立即运行** |

推荐只填写银行发送号码，先不要填写“信息包含”。短信格式可能变化，把完整原文交给 CashPulse 解析更稳。

点击右上角「下一步」，选择「新建空白自动化」。

## 第二步：添加 HTTP 请求

在动作搜索框中搜索：

```text
获取 URL 内容
```

添加该动作后，填写：

| 项目 | 值 |
|---|---|
| URL | `https://你的域名/api/v1/sms` |
| 方法 | `POST` |
| 请求体 | `JSON` |

URL 必须是 CashPulse 的公网 HTTPS 地址，例如：

```text
https://cash.example.com/api/v1/sms
```

## 第三步：设置请求头

展开「头部」，添加以下两项：

| 名称 | 值 |
|---|---|
| `Authorization` | `Bearer 你的_INGEST_TOKEN` |
| `Content-Type` | `application/json` |

`Bearer` 后面必须有一个空格：

```text
Bearer xxxxxxxxxxxxxxxxxxxx
```

不要把 Token 放进 URL，也不要写成：

```text
https://你的域名/api/v1/sms?token=xxxx
```

## 第四步：设置 JSON 请求体

把「请求体」设置为 `JSON`，添加两个字段。

### 字段 1：短信正文

| 项目 | 值 |
|---|---|
| 字段名 | `text` |
| 类型 | 文本 |
| 值 | `输入快捷指令的信息` |

操作方法：

1. 点击 `text` 右侧的灰色「文本」输入位置
2. 点击键盘上方的「选择变量」
3. 选择蓝色变量「输入快捷指令的信息」

成功后，`text` 右侧会出现一个蓝色变量块，而不是灰色的“文本”。

部分 iOS 版本可能允许继续选择变量的“详细信息”。如果界面中出现「信息内容」，可以选择它；如果没有，直接使用「输入快捷指令的信息」即可。

### 字段 2：数据来源

| 项目 | 值 |
|---|---|
| 字段名 | `source` |
| 类型 | 文本 |
| 值 | `iphone` |

最终请求体应当类似：

```json
{
  "text": "输入快捷指令的信息",
  "source": "iphone"
}
```

这里的“输入快捷指令的信息”不是固定文字，而是快捷指令中的蓝色变量。真正运行时，它会被替换成收到的短信原文。

## 第五步：保存并测试

点击右上角蓝色对勾保存自动化。

触发测试可以采用以下方式：

- 等待银行发送下一条短信
- 发起一笔极小额交易，让银行发送交易通知

收到符合条件的短信后，快捷指令应立即向 CashPulse 发起请求。

成功时，CashPulse 接口会返回：

- 新短信：HTTP `201`
- 相同短信重复提交：HTTP `200`，并返回 `"duplicate": true`

## 最终配置检查表

```text
触发器：收到 95580 发送的信息时
运行方式：立即运行

动作：获取 URL 内容
URL：https://你的域名/api/v1/sms
方法：POST

头部：
Authorization = Bearer 你的_INGEST_TOKEN
Content-Type = application/json

请求体：JSON
text   = 输入快捷指令的信息（蓝色变量）
source = iphone
```

## 常见问题

### 搜索不到“收到信息”

不要在普通快捷指令的“添加操作”页面里搜索“信息”。

“收到信息”是自动化触发器，正确入口是：

```text
快捷指令 → 自动化 → + → 信息
```

普通动作搜索中看到的「查找信息」「打开对话」「发送信息」都不是短信触发器。

### `text` 右侧一直显示灰色“文本”

这表示还没有插入短信变量。

点击灰色“文本”位置，再点击键盘上方的「选择变量」，选择「输入快捷指令的信息」。成功后会显示蓝色变量块。

### 收到短信后没有自动上传

依次检查：

1. 自动化是否仍然启用
2. 是否选择了「立即运行」
3. 短信发送号码是否与触发器完全一致
4. CashPulse 地址是否能从手机网络访问
5. `Authorization` 是否为 `Bearer`、一个空格、再加 Token
6. Token 是否与服务端的 `INGEST_TOKEN` 一致
7. HTTPS 证书是否有效

### 返回 401 Unauthorized

通常是 Token 配置错误。正确格式：

```text
Authorization: Bearer 你的_INGEST_TOKEN
```

重点检查 `Bearer` 后面的空格，以及是否误用了网页管理员密码。短信上传必须使用 `INGEST_TOKEN`。

### 返回 404 Not Found

确认 URL 包含完整路径：

```text
https://你的域名/api/v1/sms
```

不是网站首页，也不是 `/api/sms`。

### 同一条短信出现两次

快捷指令或网络可能重试请求。CashPulse 会根据相同短信正文去重，重复请求不会重复记账。

## 安全提醒

- 建议按银行号码分别创建自动化，不要采集所有短信
- 不要把验证码、私人聊天等无关短信上传到服务器
- 不要在截图、Issue、README 或日志中公开真实 `INGEST_TOKEN`
- Token 一旦泄露，应立即在服务端更换
- CashPulse 应通过 HTTPS 暴露，不要使用明文 HTTP 传输银行短信
