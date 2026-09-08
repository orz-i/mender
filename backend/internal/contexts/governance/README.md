# governance · 策略与审计

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC08、设计 G02。

- 所有权：PolicyRevision、Approval；AuditRecord
- 职责：风险决策、审批、审计与例外
- 计划负责角色：BE-A／SRE（尚未指派实际负责人）
- 候选公开合同：PolicyDecision、ApprovalDecision
- 边界：审计读模型不反向修改业务聚合

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
