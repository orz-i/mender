# Mender：供应商提交边界与结果不明协议

日期：2026-09-10。本阶段建立 **本地持久化协议**，用于未来 Executor 在真实供应商网络调用前后安全记录状态。没有实现或调用 HTTP/MCP/Agent supplier adapter、OAuth refresh、Connection credential broker、Outbox 外部投递或支付结算；生产 `cmd/worker` 仍然 `lease_dispatch_enabled=false`。

## 不变量

`BeginSubmission` 必须在任何未来网络副作用之前提交 `Attempt=submitting`、唯一 `submission_key` 与 `submission_intent_at`。只有这个事务成功后，dispatcher 才有资格把同一个 key 交给供应商。intent 之前的 lease 可以安全 `ReleaseBeforeSubmit` 或在到期后标记 `expired` 并 requeue；intent 之后绝不能使用这条安全重投路径。

供应商明确接受后，`RecordSubmitted` 在同一 execution 事务中保存 `provider_request_id`、可选 `external_task_id`、`submitted_at`，并把 Run 从 queued v1 推进为 running v2、追加 RunEvent。相同 generation、submission key 与 provider IDs 的重复确认是幂等读取，不增加 Run version。

网络 timeout、进程崩溃或 acknowledgement 不可确认时，`RecordSubmissionUnknown`／`RecoverExpired` 把 Attempt 置为 `unknown`，Job 置为 `reconciling / submission_outcome_unknown`，Run 置为 `reconciling`。若 provider IDs 已经确认，它们保留在 unknown Attempt 中供后续查询/对账使用。该 Job 不再 leaseable；旧 Worker 的 fencing token 也不能迟到写入新的 provider 结果。

## PostgreSQL

新增前向迁移 `0008_supplier_submission.sql`，不修改已落地的 0001–0007 摘要。迁移扩展 `run_attempts` submission 字段、Job reconciling 状态与索引，并重建显式命名的 Attempt CHECK。这里特意不依赖 `0007` 中 PostgreSQL 自动生成的 unnamed CHECK 名称：迁移先枚举并移除该表旧 CHECK，再以固定名称重建完整约束，避免版本/创建顺序导致遗留约束继续禁止新状态。

延迟 `submission bundle` 在事务提交时交叉验证 Run、Job 和当前 generation Attempt：queued→running 必须对应 submitted Attempt；submission uncertainty 的 reconciling 必须对应 unknown Attempt 与 reconciling Job。通用 Run `waiting_input→running` 不属于 supplier submission 首次提交，因此不会被该约束误伤。

Worker 角色在 `0008` 后新增 Run 状态所需的窄权限：读取 Runs、更新 state/version/updated_at、插入 RunEvent，以及更新 Attempt 的 submission 字段。它仍无权读取 identity、commerce、catalog、distribution、connections、canonical arguments 或 credential 数据，也不能修改 Run identity/created_at、Outbox 或 cancellation facts。既有环境迁移后必须重新运行：

```sh
pnpm db:migrate
pnpm db:grant-worker --role mender_worker
```

## 恢复与重试规则

lease 到期时先锁 Run，再锁 Job/Attempt，与协调取消保持同一 Run→Job 锁顺序。当前 Attempt 仍是 `leased` 说明没有 durable submission intent，可按原策略 expired/requeue；Attempt 已是 `submitting` 或 `submitted` 则只能 unknown/reconciling，不能自动创建新 Attempt 或再次提交。

本阶段的 `submission_key` 由调用该应用端口的未来 dispatcher 提供；Stage 3 会负责确定稳定生成规则和 Executor 端口。数据库协议本身不把“存在 key”误当成供应商支持幂等：真实适配器仍必须声明供应商幂等能力，对不支持幂等且结果不明的写操作只能查询/对账，不能盲重投。

## 验证

领域/应用测试覆盖 submitting/submitted/unknown 状态、重复 accepted 幂等、invalid key/provider facts、queued/running→reconciling 和 stale fencing。真实隔离 PostgreSQL 套件验证：`0008` 与既有 StartRun/取消/lease 迁移共存；intent 在 Run 仍 queued 时先持久；accepted provider IDs 与 running Run 同 bundle；accepted replay 不改版本；显式 timeout 与 intent/submitted lease expiry 均进入 unknown/reconciling；provider IDs 在 recovery 后保留；旧 generation 的迟到 accepted 写入被拒；直接把受理 Run 改为 running 而没有 submitted Attempt proof 无法提交。测试容器由现有所有权标签机制清理。

本阶段没有开启生产 dispatch，也没有真实供应商网络证据，因此不宣称端到端执行已经完成。下一阶段只应在此协议之上增加 Executor Dispatcher seam 与 deterministic fake executor，仍先保持真实 supplier adapter 关闭，之后再单独评审 HTTP/MCP/Agent 网络出口、credential 注入和供应商能力矩阵。
