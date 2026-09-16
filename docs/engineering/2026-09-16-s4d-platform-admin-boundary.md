# S4-D Platform Admin Operations｜S4-05 / FR-018 / T26：COMPLETE

S4-D 完成了 Platform Admin 的后端控制面边界：Workspace freeze/unfreeze、Provider quarantine/restore、受控 Incident 处置和只读 Audit export。该能力复用 S4-C 的 Platform Staff 身份，但**不把 Platform Staff 转换为 Workspace membership，也不是通用 impersonation 系统**。

## Workspace freeze

Workspace 的运行时冻结真源仍是 `identity.workspaces.disabled`，Admin 的 revision / reason / actor / updated_at 单独保存在 `identity.workspace_admin_states`。Platform Operator 只能通过 revisioned SECURITY DEFINER 函数 freeze/unfreeze，不能直接写 Identity 表。

冻结后不会删除 API Key、membership、Run delegation 或 StartRun delegation。相反，现有 Identity repository 每次重新读取这些能力时都 JOIN `identity.workspaces`，因此 API Key、Workspace membership、Run delegation 与 StartRun delegation 会立即观察到 `workspace.disabled=true` 并失效；unfreeze 后历史事实仍保留。

## Provider quarantine

Provider quarantine 是独立的 Platform operational state，真源为 `supply.provider_admin_states`。它**不是 Deployment disabled，也不是 Release emergency-disable**，不会改写 `supply.deployments.state`，也不会改写历史 `execution.run_admissions.deployment_revision`。

`supply.provider_accepts_new_work(provider)` 是唯一发布给 Runtime / Executor / MCP Connector 的布尔 SECURITY DEFINER gate；这些角色都不能直接读取 `provider_admin_states`。Admission 在解析出 immutable ToolVersion provider 后检查 gate；Supply Broker 的新 submission 和 MCP discovery 也检查 gate。显式 `quarantined` 会阻止新工作。

为了保留已有 legacy/compat provider 合同，没有 `provider_admin_states` 记录的 provider 按原行为允许；当 Provider 第一次拥有 Supply Deployment 时 trigger 建立 durable `active` state。一旦被显式 quarantine，后续新增 Deployment 不会自动覆盖该状态。

已提交 Run 的控制路径与新工作分离：`Broker.Prepare` 检查 quarantine，而 `Broker.PrepareControl` 不检查，所以 status/cancel/reconciliation 仍能使用 admission 时固定的 Deployment 和当前受控 credential reference。该语义只保留安全控制与对账能力，不表示允许新的 Provider side effect。

## Incident 与 Audit

Platform Incident 只保存 target kind/id、severity、code、reason、resolution、actor、revision 与时间，不保存 canonical arguments、credential material 或任意 tenant payload。Incident 只能 `open -> resolved`，不能删除，且 resolution 受 optimistic revision 保护。

`platform_admin_audit_events` append-only，记录 workspace freeze/unfreeze、provider quarantine/restore、incident open/resolve。Audit export 最多 500 条，仅投影 `sequence / event_kind / target_kind / target_id / target_revision / actor_user_id / reason / occurred_at`，没有 canonical arguments、credential reference 或 secret。

## Platform Staff 与数据库权限

Platform Staff 权限保持精确分离：只有 `operator` 可以 `platform:operate`；`reviewer/operator` 可以 `platform:review`；`reviewer/operator/auditor` 可以 `platform:audit`。Platform Staff 的存在不会创建 Workspace membership。

独立 `platform-admin-manager` 数据库身份只拥有 Governance schema 的受控函数 EXECUTE 和 migration metadata read。它没有 Identity、Supply、Execution、Connections、Commerce schema 的直接业务访问路径，也没有 Dangerous Operation / JIT 审批能力。Runtime / Executor / MCP Connector 只获得 Provider new-work gate EXECUTE，不获得 Provider Admin 表读取。

## T26 真实 PostgreSQL 证据

`backend/tests/integration/platform_admin_operations_test.go` 在临时真实 restricted roles 上证明：Platform Staff 零 tenant membership；Reviewer/Auditor 不能执行 Operator mutation；stale revision 被拒绝；freeze 后 API Key、membership、Run delegation、StartRun delegation 重新读取立即失效；quarantine 后 production Admission 与 new submission 被拒绝、secret provider 不被调用，而 existing-run `PrepareControl` 仍可工作；Deployment 与 historical admission pin 不被改写；restore 后新工作恢复；Incident/Audit 受控且 audit 不泄露测试中的 tenant payload/credential reference；Admin role 不能直接读取业务表。

## 明确 deferred

S4-D 不实现 S4-10 Platform Admin 前端，不实现财务调账／退款补充分录，不实现支付供应商、商业 Provider onboarding 审核、通用 impersonation 或 production deploy。虽然 FR-018 的最终平台 Admin 范围包含 reconciliation，本工作包只交付安全控制面与审计，不把尚未实现的 Commerce reconciliation 伪装成完成。

因此 **S4-D 仍不是 production-ready 声明**。下一项回到原始 roadmap 的 **canonical S4-03：账单、调账和退款补充分录**；不再为实现批次发明 S4-E 之类的新阶段编号。Payment Provider integration（S4-06）仍保持独立。
