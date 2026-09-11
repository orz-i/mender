# Mender：Provider Cancellation 与终态竞争收敛

日期：2026-09-10。本阶段补齐已经跨越 supplier submission boundary 的 Run 取消语义。**实现包含 durable cancel intent、应用层 ProviderCanceler 端口、真实 PostgreSQL 竞争收敛测试与 API intent fallback；没有 production provider cancel/status 网络 adapter，也没有访问真实外部供应商。**

## 为什么不能复用 safe cancellation

原 coordinated cancellation 只在数据库能够证明 Job 从未 lease/submission 时原子执行 `Run canceled + Job canceled + reservation release + outbox suppression`。一旦 Provider 已确认接受请求，这条路径必须返回 unsafe：远端副作用可能已经发生，不能把本地额度释放或 Job canceled 当成远端取消成功。

`MENDER_RUN_PROVIDER_CANCEL_ENABLED=true` 只在 coordinated safe-cancel 返回 `ErrUnsafeCancel` 后启用第二条路径。API 使用相同的受限 cancellation DB role 持久 `Run=cancel_requested + provider_cancel_intent=requested`，不调用供应商网络。若本地 coordinated cancel 的提交结果本身不确定，则**不会** fallback 到 provider cancel，避免双路径竞争。

## Durable intent 状态机

`0012_provider_cancellation.sql` 新增 `execution.provider_cancel_intents`：

- `requested`：用户取消意图与 provider identity/request/task handle 已持久，但还没进入网络调用边界。
- `sending`：Reconciler/Provider-control runner 已用 `FOR UPDATE SKIP LOCKED` 单次 claim；此状态在调用 ProviderCanceler **之前**提交。
- `unknown`：provider call 返回错误/明确 unknown，但无法证明远端是否收到；只持久静态原因，不保存原始 provider 错误。
- `fulfilled`：ProviderObservation=canceled 已成为终态，取消意图得到满足。
- `superseded`：ProviderObservation=succeeded/failed 先成为终态，远端结果抢先结束 Run。

一旦进入 `sending`，该 intent 永远不会自动回到 `requested`。进程在网络边界崩溃或调用返回 unknown 后，系统宁可保留 sending/unknown 并继续查询已知 provider request/task，也不会再次发送取消请求。这里与 submission protocol 一样避免 blind retry；不宣称 provider cancel exactly-once。

## ACK 与 provider status 的竞争规则

取消 ACK 不是独立于 result lifecycle 的第二套终态。ACK 会在 Reconciler transaction 中创建 `ProviderObservation=canceled`，同时把 cancel intent→fulfilled、Job→finished、Run→canceled；已有 Provider Result deferred bundle 和新的 cancel-terminal bundle共同证明完整性。

Provider status polling 在 Run=`cancel_requested` 时继续启用。因此如果 provider success/failed 先提交，ProviderResults 会把 cancel intent→superseded，再把 Job/Run 终结；之后到达的 cancel ACK 因 Run 已终态而被拒。如果 cancel ACK 先提交，Run=canceled 后迟到 success/failed observation 被 `ErrProviderAlreadyTerminal` 拒绝。数据库提交顺序决定唯一终态，没有“最后到的网络包覆盖数据库”的路径。

远端 terminal `observed_at` 可以早于本地 `sending_at`：Provider 可能在用户发起取消后、取消请求真正送达前已经成功/失败。此时 success/failed 仍可合法 supersede cancel intent。只有直接 cancel acknowledgement 路径要求 ack time 不早于 `sending_at`。

## 最小权限

Cancellation role 获得 provider-cancel intent 的 SELECT 和**请求列级 INSERT**，以及 Attempt 上 provider routing 所需的窄 SELECT。它不能 UPDATE intent，也不能写 sending/resolved/outcome/unknown 列。Reconciler role 获得 intent SELECT 和 state/sending/resolved/outcome/unknown 的列级 UPDATE，但没有 INSERT，因此无法伪造用户取消请求。Runtime、Worker、Admission、Executor 均不能访问该表。

旧 `0005` cancellation coherence 也由 `0012` 前向重定义：pre-submit safe cancel 仍必须满足 reservation release/Job canceled/outbox suppression；provider-confirmed canceled 只在存在 fulfilled cancel intent + canceled ProviderObservation + Job finished 时成立，**不会释放 admission reservation**。trigger 按调用角色可见性进入对应分支，不需要给 Reconciler run_admissions/RunEvent SELECT，也不会让 legacy runtime 为新 provider 表扩权。

## 验证

单元测试覆盖 intent single-claim、sending/unknown 不可重 claim、fulfilled/superseded、provider terminal 早于 sending 的竞争、ProviderCancelDispatcher ACK/unknown/error、原始 provider error 不持久、无 target 不触达 provider、按 provider_id 精确取消路由，以及 execution Service 只对 `ErrUnsafeCancel` fallback。

真实隔离 PostgreSQL 套件验证：旧 coordinated cancellation 与 CAS 无回归；取消请求 replay 不增加 Run version；ProviderCanceler 被调用时 intent 已是 sending；ACK 先提交得到 Run canceled/Job finished/intent fulfilled，迟到 success 被拒；success 先提交得到 Run succeeded/intent superseded，迟到 ACK 被拒；cancel call unknown 只发送一次，第二次没有可 claim target，随后 status polling canceled 收敛为 fulfilled；三条路径 Attempt 均不重建；Cancellation/Reconciler/Runtime/Worker 角色的正负权限按预期生效。测试容器由现有所有权标签机制清理。

## 仍未开放

默认 `cmd/worker` 没有 provider cancel/status production adapter；API flag 只持久 intent。下一阶段应评审 provider-specific status/cancel HTTP/MCP 能力、生产 SecretProvider/egress 配置治理、回调入口与签名验证，再进入结果 artifact 与 Commerce settlement。当前正式 WBS、ADR、G0–G5 状态不因这些测试自动改变。
