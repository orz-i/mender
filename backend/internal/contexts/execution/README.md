# execution · 执行

状态：已具备 Run 状态机、持久查询/取消、原子受理、Job/Attempt fencing、supplier submission 与 provider-result 持久协议；正式 G0/Gate 状态仍由项目评审决定。来源：BC05、设计 G02。

- 所有权：Run、Job、Attempt、ProviderObservation；Artifact 元数据模块
- 职责：Run 状态、受理幂等、Job/Attempt 租约与 fencing、supplier submission/result/cancel 状态收敛、后续执行与产物
- 计划负责角色：BE-A／BE-B（尚未指派实际负责人）
- 候选公开合同：RunView、ExecutionOutcome
- 边界：执行状态与结算状态分开

当前执行链已经具备 activation、`SKIP LOCKED` lease、heartbeat、durable submission intent、HTTP Executor seam 和受审 Supervisor。供应商 accepted 后 `Job=provider_waiting` 并立即释放本地 Worker lease；`ProviderObservation` 作为 append-only 远端结果证据，terminal observation 与 Run terminal/Job finished 由数据库延迟约束原子绑定。默认 `cmd/worker` 仍不装配生产 SecretProvider/reviewed runtime；Provider polling/cancel adapter、费用结算与 Outbox 外部投递仍属后续切片。

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
