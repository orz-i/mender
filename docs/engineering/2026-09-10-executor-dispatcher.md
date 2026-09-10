# Mender：Executor Dispatcher 应用边界

日期：2026-09-10。本阶段最初在 `0008` 的 durable submission protocol 之上增加 execution application 的 Dispatcher seam 与 deterministic fake executor 验证。后续已增加独立的 Supply Runtime Broker、hardened HTTP Executor 与 heartbeat supervisor，但**默认 `cmd/worker` 仍不装配 SecretProvider/reviewed runtime，因此不会执行真实外部供应商请求。**

## Dispatcher 顺序

Dispatcher 只接收已经取得的 `Lease`，不负责领取 Job，也不在供应商调用期间持有数据库事务。它为 `(Run, lease_generation)` 生成稳定且非秘密的 `mender.submit.<run_id>.<generation>` submission key，然后调用 `BeginSubmission`。只有 durable intent 已经成功提交后，注入的 `Executor.Submit` 才能被调用。

Executor 返回 accepted 时，Dispatcher 通过 Stage 2 的 `RecordSubmitted` 写 provider request/external task ID 与 Run running。Executor 明确返回 unknown、返回错误、或 accepted 数据无法安全持久化时，Dispatcher 不把它解释成“调用失败可重试”，而是调用 `RecordSubmissionUnknown` 进入 reconciliation。原始 executor error 文本不会写入 Attempt，避免未来供应商错误体中的 token、header 或敏感 payload 被当成持久业务事实。

非法 disposition 同样先落 reconciliation，再返回 `ErrInvalidExecutorResponse` 供调用方诊断。若 context 在供应商调用之后已经取消，Dispatcher 不创建 background context 去继续竞争写入；durable intent 保留，后续 lease-expiry recovery 将进入 unknown/reconciling。

## Fencing 与生产开关

过期或 stale lease 会在 `BeginSubmission` 阶段被拒绝，因此不会触达 Executor。accepted 结果的持久化仍携带同一 generation；若 lease 已丢失，旧 Worker 不能把迟到结果写入 Run/Attempt。

`MENDER_WORKER_DISPATCH_ENABLED=true` 现在可以表达 supervisor 配置，但默认 `RunWorker` 仍会在缺少显式 `ReviewedDispatchRuntime` 时**启动失败关闭**。只有调用方显式注入 reviewed runtime 后，supervisor 才会 activation/lease/heartbeat/dispatch；完整边界见 [Supplier Dispatch Supervisor](2026-09-10-dispatch-supervisor.md)。

## 尚未允许进入生产的能力

后续阶段已经补齐 execution-input/credential broker、HTTP SSRF/egress hardening 和 lease heartbeat supervision。仍缺少并必须单独评审：

- production secret-store adapter 与正式供应商 egress allowlist 配置/变更治理；默认 worker 不提供这些能力。
- MCP/Agent executor，以及不同 provider 的 capability 声明与协议级结果语义。
- 对供应商幂等能力的显式声明。稳定 submission key 只是 Mender 的本地标识，不能假定所有上游会执行幂等去重。
- accepted 后结果轮询/回调、上游取消、reconciliation resolution、最终结果与 Commerce settlement。

## 验证

fake executor 测试确认：Executor 被调用时 Attempt 已是 `submitting`；submission key 对相同 Run/generation 稳定；accepted 进入 running/submitted；unknown 与 executor error 进入 reconciling/unknown；raw executor error 不持久化；缺失 provider request ID 的“accepted”不会被错误视为成功；非法 disposition 会进入 reconciliation 并暴露合同错误；过期 lease 在 Executor 调用之前即被拒绝。

Stage 2 的真实 PostgreSQL 套件继续提供 submission bundle 与 fencing 的数据库证据；本阶段收尾还会重跑真实 PostgreSQL、全仓库 `pnpm check`、非缓存 Go race 和 `git diff --check`。正式 WBS、ADR、G0–G5 状态不因这些测试自动改变。
