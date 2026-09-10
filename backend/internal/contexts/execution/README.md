# execution · 执行

状态：已具备 Run 状态机、持久查询/取消、原子受理入口、Job/Attempt 租约控制基础；正式 G0/Gate 状态仍由项目评审决定。来源：BC05、设计 G02。

- 所有权：Run、Attempt；Artifact 元数据模块
- 职责：Run 状态、受理幂等、Job/Attempt 租约与 fencing、后续执行与产物
- 计划负责角色：BE-A／BE-B（尚未指派实际负责人）
- 候选公开合同：RunView、ExecutionOutcome
- 边界：执行状态与结算状态分开

当前 Worker 控制只覆盖本地 SQL Job/Attempt：显式 activation、短事务 `SKIP LOCKED` lease、heartbeat、到期恢复和 fencing token。`cmd/worker` 默认 idle；显式启用后目前也只恢复过期租约，不领取新任务、不改变 Run 为 running、不调用供应商。真实请求提交、上游结果/取消、费用结算与 Outbox 外部投递仍属后续阶段。

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
