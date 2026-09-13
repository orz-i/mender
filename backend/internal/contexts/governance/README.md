# governance · 策略与审计

状态：已实现 Catalog publication approval + append-only audit history 的 Alpha 子集；G0 仍待冻结。来源：BC08、设计 G02。

- 所有权：PolicyRevision、Approval；AuditRecord
- 职责：风险决策、审批、审计与例外
- 计划负责角色：BE-A／SRE（尚未指派实际负责人）
- 候选公开合同：PolicyDecision、ApprovalDecision
- 边界：审计读模型不反向修改业务聚合
- 当前 Alpha：Workspace-scoped maker/checker publication approval；绑定精确 Catalog/Toolset revision，禁止自审，批准后由 Catalog/Distribution 发布事务原子消费。Governance 同时拥有 append-only publication audit timeline，记录 submitted/decision/expiry/revision drift/consume/commit/retire 治理事实；Admin history 只通过受限 reviewer role 做有界只读查询，sequence/revision 对 HTTP 保持 exact decimal string。Audit 不记录 Connection secret、执行参数、Artifact body 或 quota/payment amount。尚未实现多级审批、通知、外部审计导出、SIEM 或组织级治理 RBAC。

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
