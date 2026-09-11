# Mender：Provider Result Lifecycle

日期：2026-09-10。本阶段在 supplier submission/fencing 之后建立异步 Provider Result 的持久证据模型。后续 `0011` 与 Provider Reconciler 已加入按 provider identity 路由的状态查询应用编排；**仍没有 production provider polling/callback/cancel adapter，也没有访问真实供应商。**

## Accepted 后释放 Worker lease

`0010_provider_results.sql` 将 supplier acknowledgement 的最终本地状态从 `Job=leased` 改为 `Job=provider_waiting`。`RecordSubmitted` 仍要求当前 fencing token、有效 lease 和 `Attempt=submitting`，但在 provider request ID 成功持久化的同一事务中清空 `lease_owner/lease_until`。因此远端异步任务可能运行数分钟或数小时，也不会靠本地 Worker heartbeat 维持所有权。

这也修正了 `0008` 的临时基础语义：已经 accepted 的 Attempt 不再因为历史 lease 到期被错误转成 `unknown/reconciling`。只有 acknowledgement 尚不确定的 `submitting` 才使用 lease-expiry recovery；已经拥有 provider request ID 的 `submitted` 进入 result reconciliation 生命周期。

## Append-only ProviderObservation

`execution.provider_observations` 记录 `(workspace, run, observation_id)`、Attempt、provider request/external task IDs、`pending/succeeded/failed/canceled` 状态、受限结果 JSON/错误码和 observation 时间。表启用 FORCE RLS、PUBLIC 无权限，既有 Runtime/Worker/Admission/Cancellation/Executor 角色的启动验证都要求无法访问它。

相同 observation ID 只有所有业务事实语义一致时才是 replay；相同 ID 的不同状态、结果或时间会冲突。新的 observation 时间不能早于已持久化 observation。一旦 Run/Job 已终态，不允许追加不同 observation。这里提供的是 monotonic evidence，不宣称供应商本身严格按时间顺序生成状态。

terminal observation 必须和同一事务中的 `Run=succeeded/failed/canceled`、`Job=finished`、相同 `observed_at` 完整配对。反向也成立：不能直接把 Run 或 Job 改成终态而没有 provider observation。`canceled` 仍遵守现有 Run 状态机，只能从 `cancel_requested/reconciling` 确认；本切片没有提前放宽 running→canceled。

## Reconciler 最小权限

新增操作员命令：

```sh
pnpm db:grant-reconciler --role mender_reconciler
```

Reconciler 只读 Run/Job/Attempt/result observation，追加 observation/RunEvent，并只更新 Run 的 state/version/updated_at 与 Job 的 terminal 列。它不能读取 admitted arguments、Identity、Commerce、Connections、Supply、Outbox，也不能修改 Attempt 或已有 observation。真实 PostgreSQL 发现 `SELECT ... FOR SHARE` 会要求不必要的额外权限，因此历史 Attempt 只做 RLS SELECT；事务通过 Run→Job 行锁串行终态收敛，不扩大 Reconciler 权限。

## 验证

隔离 PostgreSQL 套件验证：accepted submission 原子释放 Worker lease；旧 lease expiry 对 `provider_waiting` 不产生 recovery；pending observation 不改变 Run/Job；terminal success 原子生成 observation + finished Job + terminal Run/Event；完全相同 observation replay 不增加 Run version；同 ID 不同事实、乱序 observation、terminal 后新 observation 均拒绝；Reconciler 无法读 arguments/Identity/Commerce/Connections/Supply、无法改 Attempt 或删除 observation；没有 provider evidence 的直接 terminal Run update 无法提交。测试容器由现有所有权标签机制清理。

后续切片已在此协议之上增加 provider status query/reconciliation 应用端口与 fake provider，并通过真实 PostgreSQL + 内存 provider reader 验证；完整说明见 [Provider Reconciliation](2026-09-10-provider-reconciliation.md)。真实公网 provider polling 仍未启用。
