# Mender：Executor Dispatcher 应用边界

日期：2026-09-10。本阶段在 `0008` 的 durable submission protocol 之上增加 execution application 的 Dispatcher seam 与 deterministic fake executor 验证。**没有生产 HTTP/MCP/Agent executor，没有读取真实 arguments/Connection credential，没有开启网络出口，也没有改变 `cmd/worker` 的默认或显式启用行为。**

## Dispatcher 顺序

Dispatcher 只接收已经取得的 `Lease`，不负责领取 Job，也不在供应商调用期间持有数据库事务。它为 `(Run, lease_generation)` 生成稳定且非秘密的 `mender.submit.<run_id>.<generation>` submission key，然后调用 `BeginSubmission`。只有 durable intent 已经成功提交后，注入的 `Executor.Submit` 才能被调用。

Executor 返回 accepted 时，Dispatcher 通过 Stage 2 的 `RecordSubmitted` 写 provider request/external task ID 与 Run running。Executor 明确返回 unknown、返回错误、或 accepted 数据无法安全持久化时，Dispatcher 不把它解释成“调用失败可重试”，而是调用 `RecordSubmissionUnknown` 进入 reconciliation。原始 executor error 文本不会写入 Attempt，避免未来供应商错误体中的 token、header 或敏感 payload 被当成持久业务事实。

非法 disposition 同样先落 reconciliation，再返回 `ErrInvalidExecutorResponse` 供调用方诊断。若 context 在供应商调用之后已经取消，Dispatcher 不创建 background context 去继续竞争写入；durable intent 保留，后续 lease-expiry recovery 将进入 unknown/reconciling。

## Fencing 与生产开关

过期或 stale lease 会在 `BeginSubmission` 阶段被拒绝，因此不会触达 Executor。accepted 结果的持久化仍携带同一 generation；若 lease 已丢失，旧 Worker 不能把迟到结果写入 Run/Attempt。

仓库新增 `MENDER_WORKER_DISPATCH_ENABLED=false` 配置说明，但当前值设置为 `true` 会**启动失败关闭**，错误明确说明没有 production executor。`RunWorker` 仍只执行 expired lease recovery，并持续记录 `lease_dispatch_enabled=false`。因此本阶段不会因为增加 application seam 而意外激活 blocked Job、领取 queued Job 或调用供应商。

## 尚未允许进入生产的能力

真实供应商执行前仍缺少并必须单独评审：

- lease acquisition 与长调用 heartbeat supervision；dispatcher 当前处理一个已 lease Attempt，不负责并发 worker loop。
- arguments/artifact 的受控读取端口，以及 Connection credential broker；当前 ExecutorSubmission 不含 payload 或 secret。
- HTTP/MCP/Agent 各协议的 provider capability 声明、timeout/cancellation、SSRF/egress、header 注入与响应大小限制。
- 对供应商幂等能力的显式声明。稳定 submission key 只是 Mender 的本地标识，不能假定所有上游会执行幂等去重。
- accepted 后结果轮询/回调、上游取消、reconciliation resolution、最终结果与 Commerce settlement。

## 验证

fake executor 测试确认：Executor 被调用时 Attempt 已是 `submitting`；submission key 对相同 Run/generation 稳定；accepted 进入 running/submitted；unknown 与 executor error 进入 reconciling/unknown；raw executor error 不持久化；缺失 provider request ID 的“accepted”不会被错误视为成功；非法 disposition 会进入 reconciliation 并暴露合同错误；过期 lease 在 Executor 调用之前即被拒绝。

Stage 2 的真实 PostgreSQL 套件继续提供 submission bundle 与 fencing 的数据库证据；本阶段收尾还会重跑真实 PostgreSQL、全仓库 `pnpm check`、非缓存 Go race 和 `git diff --check`。正式 WBS、ADR、G0–G5 状态不因这些测试自动改变。
