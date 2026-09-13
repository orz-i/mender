# governance · 策略与审计

状态：已实现 Catalog publication approval + append-only audit history + publication policy/risk rules 的 Alpha 子集；G0 仍待冻结。来源：BC08、设计 G02。

- 所有权：PolicyRevision、Approval；AuditRecord
- 职责：风险决策、审批、审计与例外
- 计划负责角色：BE-A／SRE（尚未指派实际负责人）
- 候选公开合同：PolicyDecision、ApprovalDecision
- 边界：审计读模型不反向修改业务聚合
- 当前 Alpha：Workspace-scoped maker/checker publication approval；绑定精确 Catalog/Toolset revision，禁止自审，批准后由 Catalog/Distribution 发布事务原子消费。Governance 同时拥有 append-only publication audit timeline。Publication policy 使用不可变版本化声明式规则，从持久化 ToolVersion/Toolset facts 产生绑定 exact target/policy revision 的 allow/deny PolicyDecision；deny 在 submit/review/publish 服务端关口 fail closed，但不会绕过既有 technical preflight 或 maker/checker。Policy 管理使用独立 least-privilege DB role，不开放脚本/表达式策略；治理数据不记录 Connection secret、执行参数、Artifact body 或 quota/payment amount。尚未实现运行时执行策略、危险动作参数审批、多级审批、通知、外部审计导出、SIEM 或组织级治理 RBAC。

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
