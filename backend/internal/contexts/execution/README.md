# execution · 执行

状态：已具备 Run 状态机、持久查询/取消、原子受理、Job/Attempt fencing、supplier submission 与 provider-result 持久协议；正式 G0/Gate 状态仍由项目评审决定。来源：BC05、设计 G02。

- 所有权：Run、Job、Attempt、ProviderObservation；Artifact 元数据模块
- 职责：Run 状态、受理幂等、Job/Attempt 租约与 fencing、supplier submission/result/cancel 状态收敛、后续执行与产物
- 计划负责角色：BE-A／BE-B（尚未指派实际负责人）
- 候选公开合同：RunView、ExecutionOutcome
- 边界：执行状态与结算状态分开

当前执行链已经具备 activation、`SKIP LOCKED` lease、heartbeat、durable submission intent、HTTP Executor seam、受审 Supervisor、Provider Reconciler 与 provider-cancel intent。供应商 accepted 后 `Job=provider_waiting` 并立即释放本地 Worker lease；Attempt 保存实际 `provider_id + provider_request_id`。Provider Reconciler 只选择具有完整路由证据的 submitted/unknown Attempt，通过 Supply public status port 查询并把结果写成 append-only `ProviderObservation`。用户对已跨 supplier 的 Run 取消时，可在显式 API feature flag 下持久 `cancel_requested + provider_cancel_intent`；外部取消必须先 claim 为 `sending`，一旦可能跨过网络边界就绝不自动重新发送。Provider canceled/succeeded/failed 最终按数据库提交顺序收敛为 fulfilled 或 superseded。默认 `cmd/worker` 仍不装配 production provider status/cancel reader；费用结算与 Outbox 外部投递仍属后续阶段。

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
