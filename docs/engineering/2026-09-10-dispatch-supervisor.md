# Mender：Supplier Dispatch Supervisor

日期：2026-09-10。本切片在 durable submission protocol、Supply Runtime Broker 与 hardened HTTP Executor 之上增加 Execution-owned dispatch supervisor。它负责 activation、lease、heartbeat 和 Dispatcher 生命周期，但 **默认 `cmd/worker` 仍没有 SecretProvider，也不会创建 reviewed supplier runtime，因此不会执行真实外部供应商请求。**

## 编排顺序与 fencing

`DispatchSupervisor.DispatchOne` 先按显式 deployment revision 集合激活可执行 Job，再领取一个 fenced lease。只有 lease 成功后才创建 heartbeat ticker，随后 Dispatcher 在独立 goroutine 中执行；Dispatcher 仍保持既有顺序：先 durable `submission intent`，再调用注入 Executor。

heartbeat 与 Dispatcher 并行。每次 heartbeat 都通过当前 Workspace/Run/worker/generation 更新 Job 与 Attempt 的 lease。若 heartbeat 丢失或 fencing 被新 generation 取代，Supervisor 取消 dispatch context，并且**不会调用 `ReleaseBeforeSubmit`**。若 intent 已经持久化，现有 lease-expiry recovery 会把 submitting/submitted Attempt 转成 `unknown`，Job/Run 转成 `reconciling`，不盲目 requeue/re-submit。

只有 heartbeat ticker 在 Dispatcher 启动前无法建立时，才调用 `ReleaseBeforeSubmit`；此时尚未进入 submission intent，可以证明没有供应商副作用。该规则用 deterministic fake ticker/control 测试覆盖。

## Bootstrap 与默认 fail-closed

Worker 配置现在能表达 dispatch 参数：`MENDER_WORKER_DEPLOYMENT_REVISIONS`、`MENDER_WORKER_LEASE_TTL`、`MENDER_WORKER_HEARTBEAT_INTERVAL`、`MENDER_WORKER_ACTIVATION_LIMIT`。heartbeat 必须小于 lease TTL 的一半；deployment revisions 必须显式、唯一且合法。

`MENDER_WORKER_DISPATCH_ENABLED=true` 本身**不足以开启执行**。默认 `RunWorker` 不注入 `ReviewedDispatchRuntime`，因此会在数据库连接和任何网络调用前失败关闭。未来只有显式构造受审 runtime 并调用 `RunWorkerWithReviewedRuntime` 才能进入 dispatch 路径。

`BuildSupplierHTTPExecutor` 提供该组合的基础构件：executor material 使用独立 `mender_executor` 数据库角色，必须与 Worker 指向同一数据库但使用不同角色；调用方还必须显式提供 `SecretProvider` 与 HTTP egress allowlist。Builder 不从环境变量读取 secret，也不把 Worker lease role 扩权为可读 arguments/credential reference。

当前 worker dispatch loop 有意限制为**单进程最多一个活动 submission**。这是安全优先的初始策略；增加并发度需要单独评审限流、供应商配额、公平性、shutdown/drain 和每 Workspace 隔离后再实现。

## 错误处理

`ErrNoWork` 不视为异常。已经进入 reconciliation 的 `ErrExecutorOutcomeUnknown`、无效 provider 合同以及 heartbeat lease loss 都不会触发即时重试；Worker 可继续后续周期。数据库/控制面不可用等不能证明安全继续的错误仍使循环失败关闭。

HTTP Executor 与 Supply ACL 不把原始 provider response/error、secret 或 credential reference写入日志。Worker 日志只记录 Workspace、Run 和非秘密 submission key；默认命令不存在真实 supplier runtime，因此本任务没有外网调用证据。

## 验证范围与剩余能力

Supervisor 单元测试覆盖正常 heartbeat、heartbeat loss 取消、ticker-before-submit 安全 release、无 Job 与配置校验。最终阶段还会重跑真实隔离 PostgreSQL、全仓库 `pnpm check`、`go test -race -count=1 ./...` 与 `git diff --check`。

仍未实现 provider result polling/callback、reconciliation resolution、上游取消、MCP/Agent executor、secret-store production adapter、Outbox delivery 与 Commerce settlement。accepted submission 在后续 lease expiry 时仍按既有协议进入 reconciliation，直到结果处理阶段补齐。正式 WBS、ADR、G0–G5 状态不因本切片自动推进。
