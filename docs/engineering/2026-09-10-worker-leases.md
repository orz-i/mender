# Mender：Worker Job/Attempt 租约与 fencing 基础

日期：2026-09-10。本阶段在公开 StartRun 的 blocked Job 基础上实现 execution 自有的本地 Worker 控制面：Job/Attempt 生命周期、SQL lease、heartbeat、lease expiry recovery 与单调 fencing generation。**没有接入真实供应商 HTTP/MCP/Agent、OAuth refresh、外部副作用、Outbox 外部投递或支付结算。**

## 领域与状态边界

`execution.Job` 的租约基础最初区分 `blocked / queued / leased / canceled`；`0008` 进一步增加 `reconciling / submission_outcome_unknown`，用于“已经存在供应商提交意图但结果不能确认”的不可自动重投状态。StartRun 仍创建 `blocked / executor_not_configured`；只有显式提供允许的 deployment revision 时，WorkerControl 的 activation 用例才能将对应 Workspace 的 Job 变成 `queued`。`cmd/worker` 当前没有调用 activation 或 lease，因此启用控制进程不会自动把现有 blocked Job 变成可执行任务。

领取 Job 只代表本地执行权，不代表已经把请求提交给供应商，也不把 Run 改成 `running`。每次领取同时创建一个不可变编号的 `run_attempts` 事实；`attempt_no == lease_generation`，generation 每次新领取严格递增。heartbeat 只延长当前 generation 的 lease。提交供应商之前若本地决定暂不执行，可以 release 回 queued；Attempt 标记 `released`。进程崩溃后，只有 `lease_until <= now` 的 Attempt 才能标记 `expired` 并把 Job 恢复为 queued；达到 max_attempts 后进入 `blocked / attempt_limit_reached`。

所有 lease mutation 都要求 Workspace、Run、worker_id 和 `lease_generation` 一致。旧 Worker 即使仍持有历史数据，也不能 heartbeat、release 或覆盖新 generation。该 fencing 只保护 Mender 数据库写入；未来一旦真实供应商请求已经发出，lease 过期并不能撤回网络副作用，因此该边界不能被用于非幂等请求的盲目重投。

## PostgreSQL 与权限

`0007_worker_leases.sql` 是新增前向迁移，没有修改 0001–0006 的历史内容。它扩展 `execution.jobs` 的 available_at、priority、lease_owner、lease_until、lease_generation、attempt_count、max_attempts、updated_at，并新增 `execution.run_attempts`、ready/expired 索引、FORCE RLS 和延迟 lease/Attempt bundle 约束。

领取使用 Workspace-scoped 短事务和 `FOR UPDATE SKIP LOCKED`：锁住候选行后立即写 Job lease 与 matching Attempt，然后提交；网络调用不在该事务内。heartbeat、release 与 expiry recovery 同样是短事务。租约/Attempt 的延迟约束保证最终提交时 leased Job 与 leased Attempt 的 generation、owner、lease_until 一致。

Worker 使用独立受限数据库角色。操作员先创建无特权 login role，再执行：

```sh
pnpm db:migrate
pnpm db:grant-worker --role mender_worker
```

如果 admission role 在 `0007` 之前已经授权，还需重新运行 `pnpm db:grant-admission --role <role>`。新版授权流程会先撤销 `execution.jobs` 的旧整表 INSERT，再改为列级 INSERT；否则历史 ACL 可能自动覆盖 0007 新增的 lease/fencing 列。API startup 的 AdmissionRole 校验也会拒绝这种过宽旧授权。

应用 `0008` 后需再次执行 `pnpm db:grant-worker --role <role>`。Worker 角色仍只围绕 execution 控制面：除 Job/Attempt 与 admission 的三列部署投影外，新增 `execution.runs` SELECT、仅 state/version/updated_at UPDATE，以及 `execution.run_events` 仅 INSERT，以原子记录 accepted/unknown submission 状态。它没有 identity、commerce、catalog、distribution、connections schema 使用权，不能读取 canonical arguments/idempotency/credential/budget/price，不能改 Run identity/created_at，也不能访问 Outbox 或 cancellation facts。数据库延迟 bundle 要求 Run、Job、Attempt 对同一 generation 的提交状态匹配；Worker startup 会执行迁移摘要和角色合同核验，错误配置失败关闭。

## 当前 Worker 进程

默认 `MENDER_WORKER_CONTROL_ENABLED=false`，进程保持 idle 且不连接数据库。显式启用至少需要 `MENDER_WORKER_DATABASE_URL`、`MENDER_WORKER_ID` 和 1–64 个 `MENDER_WORKER_WORKSPACES`；可配置 100ms–1m 的 poll interval。

当前循环仅对这些 Workspace 执行 expired lease recovery，并明确记录 `lease_dispatch_enabled=false`。应用层和 PostgreSQL 适配器已经有 `Activate`、`LeaseOne`、`Heartbeat`、`ReleaseBeforeSubmit`、`BeginSubmission`、`RecordSubmitted`、`RecordSubmissionUnknown` 和 `RecoverExpired`；其中只有 recovery 被生产 bootstrap 调用。没有 executor dispatcher 时，生产进程不会新领取任务，也不会自行产生 submission intent 或调用供应商。

## 验证

领域/应用测试覆盖 Job 状态、Attempt 不变量、generation 单调递增、heartbeat、release、到期限制、attempt limit 和 stale token 映射。真实隔离 PostgreSQL 套件验证：migration 0007 可与此前迁移和 StartRun/协调取消共存；按 deployment revision 和 Workspace 激活；16 个并发领取只有一个 winner；Job lease 与 Attempt 原子提交；heartbeat 延长；release 后下一次领取 generation 递增；崩溃式 lease expiry 恢复并持久标记 Attempt expired；旧 generation 不能覆盖新 lease；Worker 角色的 RLS 和最小权限成立。测试容器由现有隔离编排器按所有权 label 清理。

后续 `0008` 已补足 T10 的本地数据库协议部分：intent 必须先持久化；intent 后 lease 到期不再 requeue，而是进入 unknown/reconciling；accepted provider IDs 与 Run running 同 bundle 持久化。它仍不等于供应商“恰好一次”，因为真实网络适配器尚未接入。完整证据见 [供应商提交协议](2026-09-10-supplier-submission.md)。正式 WBS、ADR-020 和 G0–G5 状态不因代码与测试自动改变。
