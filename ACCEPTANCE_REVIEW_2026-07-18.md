# CashPulse 二次验收与封版整改书

- 验收日期：2026-07-18
- 验收基线：`1398bab`（`Fix Review P1/P2: idempotent SMS, auth, analytics, pagination`）
- 验收性质：第二轮回归，只提出修改意见，不包含实现代码
- 当前结论：主体整改合格；修复下述唯一 P1 后即可封版

## 1. 本轮结论

上一轮的刷新、历史月份预算、Session 重启保持、CORS、分页、CSV 时区等主要问题已经完成整改。

目前不建议立刻封版的原因只剩一个：短信幂等虽然已经能阻止重复交易入库，但在并发和进程中断场景下会把尚未完成的 `pending` 记录返回成成功，并可能让该短信以后永远无法重新处理。这是财务流水可靠性问题，仍按 P1 处理。

本轮不要求继续重构页面或扩展产品功能。P1 修复并通过本文测试后即可结束项目；其余问题均可作为非阻断清理项。

## 2. 已通过的验收项

### 2.1 自动化检查

- `go test -race ./...`：通过。
- `go vet ./...`：通过。
- `cd web && npm run build`：通过。
- Git 工作区在验收开始和结束时均无业务文件改动。

### 2.2 业务黑盒回归

- 历史月份预算：保存 `2026-06` 后可立即按 `2026-06` 读回，金额一致。
- Session 持久化：使用同一 SQLite 数据库重启服务后，原 Cookie 仍能访问管理接口。
- 登录全局锁：连续 8 次错误密码后新登录返回 `429`；此前已登录的 Session 仍能访问流水接口并返回 `200`。
- 流水分页：测试库 57 笔数据，`limit=50&offset=0` 返回 50 笔，`offset=50` 返回 7 笔，两个响应的 `total` 都是 57。
- 顺序重复短信：相同正文连续提交只产生一笔交易，重复请求返回同一个 `raw_sms_id` 和交易。
- CORS：非白名单 Origin 不返回允许跨域头；开发白名单 Origin 正常返回凭证 CORS 头。
- 非法流水日期：返回 `400`。
- 未来月份：前端月份控件已经禁止进入未来月份。

## 3. P1：短信 pending 被伪装成成功，崩溃后无法恢复

### 3.1 实测结果

同时发起 8 个正文完全相同的短信上报请求：

- SQLite 中最终只有一条交易，唯一指纹确实阻止了重复计账。
- 其中一个并发请求返回了：

```json
{
  "raw_sms_id": 2,
  "status": "ok",
  "duplicate": true
}
```

该响应没有 `transaction`。这不是最终成功结果，而是重复请求读取到了第一个请求刚写入、尚未处理完成的 `pending` 状态。

### 3.2 根因

当前流程分成多个独立数据库操作：

1. 插入 `raw_sms(status=pending)`；
2. 在 Service 中解析短信；
3. 插入 `transactions`；
4. 将 `raw_sms` 更新为 `ok`。

重复请求命中唯一指纹后会立即读取现有记录。若记录仍是 `pending`，`IngestSMS` 会把它强行改成响应中的 `ok`，然后返回空交易。

这会带来两个确定的问题：

- 并发调用者收到语义错误的成功响应。
- 如果第一个请求在步骤 1 之后崩溃，数据库会永久留下 pending；以后所有重试都命中该指纹并返回“成功但无交易”，真实流水可能漏记。

### 3.3 推荐修法：解析在前，最终落库使用一个事务

这是本项目当前规模下最简单、最可靠的实现，不需要引入消息队列或复杂分布式锁。

#### 第一步：先在事务外解析

Service 收到正文后先计算规范化指纹并执行 Parser，得到以下三种最终结果之一：

- `ok`，携带待写入的 Transaction；
- `ignored`，携带忽略原因；
- `failed`，携带解析错误。

解析阶段不要提前持久化 `pending`。如果进程在解析时退出，请求没有得到成功响应，客户端重试即可，不会留下阻止重试的数据库记录。

#### 第二步：增加原子持久化 Store 方法

建议新增一个表达完整用例的方法，例如：

```go
PersistIngestResult(ctx, text, source, fingerprint, parseResult) (result, error)
```

该方法内部开启一个 SQLite 事务，并在同一事务内完成：

1. 按 fingerprint 尝试插入 `raw_sms`。
2. 如果插入成功：
   - `ok`：插入 transaction，并把 raw_sms 写成最终 `ok`；
   - `ignored/failed`：直接把最终状态和原因写入 raw_sms；
   - 提交事务。
3. 如果 fingerprint 已存在：
   - 不再创建新交易；
   - 读取既有 raw_sms 及其 transaction；
   - 返回数据库里已经提交的最终结果。

任何一步失败都必须回滚整个事务。不能留下“raw_sms 已提交、transaction 未提交”的半成品。

现有 Store 使用单个 SQLite 连接，多个 goroutine 会自然串行；SQLite 在多进程情况下也会串行化写事务。唯一索引负责最终兜底。

#### 第三步：增加交易侧唯一约束

增加：

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_raw_sms_id
ON transactions(raw_sms_id);
```

即使以后业务代码发生回归，同一个 raw SMS 也不能关联两条交易。

迁移前应先检查是否存在重复的 `raw_sms_id`。如果存在，不要静默删除财务数据；启动失败并打印重复 ID，交给人工确认。

#### 第四步：处理旧版本遗留 pending

新流程正常情况下不应持久化 pending，但升级前数据库可能已经存在 pending。

必须选择一种明确策略：

- 推荐：启动时或通过一次性迁移重新解析旧 pending，并原子补齐最终状态/交易；或
- 最低要求：重复请求命中 pending 时返回 `202 Accepted` 和 `status: pending`，绝不能改成 `ok`。

如果采用重新处理，必须依赖 `transactions(raw_sms_id)` 唯一约束保证不会补出重复交易。

### 3.4 明确禁止的修法

- 不得继续把 `pending` 转成 `ok`。
- 不得通过简单 `sleep` 后再查询来假装解决竞态。
- 不得只在 Go 内存中加 mutex；服务重启或多实例时无效。
- 不得删除 fingerprint 唯一约束后依赖“快捷指令通常不会重复”。
- 不得在没有恢复策略的情况下永久保留 orphan pending。

### 3.5 HTTP 响应语义

建议统一为：

- 新短信处理完成：`201 Created`，`status=ok|failed|ignored`。
- 已有且已完成的重复短信：`200 OK`，`duplicate=true`，成功交易必须带相同的 transaction。
- 仅用于兼容旧数据或确有异步处理时的 pending：`202 Accepted`，`status=pending`，不得声称成功。
- 数据库事务失败：`5xx`，让客户端明确知道需要重试；不要返回业务成功。

### 3.6 必须增加的自动化测试

#### Service 并发测试

- 使用 start barrier 同时启动至少 20 个 goroutine。
- 所有 goroutine 提交完全相同的短信。
- 最终断言：raw_sms 一条、transaction 一条。
- 所有最终成功响应都必须带 transaction，并且 transaction ID 相同。
- 禁止出现 `status=ok && transaction=nil`。
- 测试必须在 `go test -race` 下通过。

#### Store 事务回滚测试

- 在测试数据库创建一个会让 transaction INSERT 失败的临时 trigger，或通过明确的故障注入制造失败。
- 调用原子持久化方法，断言事务返回错误。
- 断言该 fingerprint 没有留下半成品 raw_sms，且 transaction 数为零。
- 移除故障后使用相同正文重试，断言能够成功落一条交易。

#### 旧 pending 回归测试

- 手工插入一条旧 `pending` raw_sms。
- 再次提交相同短信。
- 根据选定策略断言它被安全恢复，或者返回 `202/pending`。
- 绝不能返回 `200/201 + status=ok + transaction=nil`。

#### API 状态码测试

- 新建返回 201。
- 完成态重复返回 200。
- pending 若保留则返回 202。
- 持久化失败返回 5xx。

### 3.7 P1 最终验收标准

以下条件全部满足即可关闭 P1：

- 原始短信、交易和最终状态不存在可见的半完成提交。
- 并发重复提交只产生一条交易。
- 任意 `status=ok` 响应都带有效 transaction。
- 首请求在任意持久化步骤失败后，相同短信仍可安全重试。
- 旧 pending 有明确、可测试的恢复或 202 语义。
- 相关测试通过 `go test -race ./...`。

## 4. 非阻断清理项

这些问题不阻止封版，但建议对方在修 P1 时顺手处理；不要因此继续扩大项目范围。

### P2-1 后端未来月份返回今天的数据

直接请求未来月份时，后端把起止日期都改成今天，因此未来月份响应会包含今天的真实流水。

前端已经禁止选择未来月份，所以正常 UI 不受影响。后端建议对未来 `month` 直接返回 `400 future month not allowed`；自定义区间若整体在未来，也应返回 400 或明确空结果，不能伪装成今天。

### P2-2 删除无效的 SESSION_SECRET

Session 已经持久化在 SQLite，并且服务重启后保持登录已通过实测。但 `Config.SessionSecret`、随机 secret 生成逻辑及环境示例仍然存在，运行时没有任何代码使用该值。

建议删除：

- `Config.SessionSecret`；
- `crypto/rand`、`encoding/hex` 相关无效逻辑；
- `.env.example` 和 `deploy/env.example` 中的 `SESSION_SECRET`；
- “未设置会导致重启掉登录”的过期注释。

### P2-3 设置页异步错误没有完全统一

创建目标、创建/删除规则、删除标签和 CSV 导出等操作仍有未捕获的异步异常。建议统一一个 `runAction`/`try-catch` 模式，失败显示错误 banner，只有成功后才刷新或显示成功提示。

### P3-1 全部流水重复显示数量

流水页连续显示了“搜索结果 N 笔”和“已显示 N / 共 M 笔”两行近似信息。保留后一行即可；搜索词可合并进同一行。

### P3-2 文档与 Token 残留

- README 仍写浏览器可使用 Token 登录，但当前登录页只有密码输入。
- README 配置表重复列出 `INGEST_TOKEN`。
- 前端仍保留 `getToken/setToken/tokenInput` 等无 UI 入口的历史代码。

产品规则已经确定为“网页使用密码 Session，ADMIN_TOKEN 只给脚本”，建议让文档和前端代码完全一致。

### P3-3 API 输入校验

建议至少限制：

- budget month 只能是 `YYYY-MM` 或明确支持的 `*`；
- budget kind 只能是 `consume|transfer|all`；
- rule field/op 只能是支持的枚举；
- from 日期不得晚于 to，或明确自动交换并写入接口约定。

### P3-4 格式检查

`git diff --check` 当前报告 `internal/store/store.go` 存在一处行尾空格。提交前清理即可。

## 5. 封版检查单

对方完成 P1 后，只需要执行以下最后一轮：

```bash
go test -race ./...
go vet ./...
cd web && npm run build
git diff --check
```

然后使用临时数据库做一次：

1. 20 并发相同短信；
2. 服务重启后原 Session 继续有效；
3. 历史月份预算保存并刷新；
4. 51 条以上流水加载第二页；
5. 触发登录全局锁后，已有 Session 仍能访问。

如果 P1 验收标准全部满足，并且上述命令通过，可将 CashPulse 定义为最终稳定版并结束项目。
