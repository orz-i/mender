# Mender S4-B Release Governance / T23 Boundary

日期：2026-09-16  
机器证据：[s4b-release-governance-evidence.json](s4b-release-governance-evidence.json)

## 决议

**S4-B Release Governance / T23：COMPLETE**

本工作包把 S4-A 已发布的受控供给接入发布路由治理：`ReleasePlan` 保存精确的 stable/candidate 与发布生命周期，`release_routes` 保存只影响**新 Admission** 的当前路由快照。已发布 ToolVersion 仍然不可变，历史 `execution.run_admissions.deployment_revision` 仍是 Run 的执行版本真源，发布切换不会迁移旧 Run。

## Canary、Drain 与 Rollback

当前 canary 是 **Workspace + Toolset + ToolVersion cohort** 级切换：命中该发布组的新 Run 选择 candidate；它不是百分比随机分流，也没有自动指标驱动的 promotion。这个边界与设计中的 Workspace/Toolset 灰度分组一致，同时避免在 Admission 中引入随机路由与不可复现选择。

- `canary`：新 Admission 固定 candidate deployment；
- `promote`：candidate 成为 route snapshot 的新 stable，但不会改写 ToolVersion；
- `draining`：新 Admission 回到该计划进入 canary 前的 stable，让 candidate 上已开始的工作继续按原 pin 收敛；
- `rollback`：新 Admission 使用 prior stable，旧 Run 的 `deployment_revision` 不变；
- route 与 plan 都有 revision，切换使用数据库事务和 CAS 条件，不靠浏览器提交 state/revision。

真实 PostgreSQL T23 drill 已证明：canary 期间生产 Admission Resolver 选择 candidate 并写入既有 Run admission pin；随后 drain/rollback 后，新解析回 stable，但历史 Run 仍保留 candidate revision。Promotion 同样只推进 route stable，`catalog.tool_version_management.deployment_revision` 保持原值。

## Emergency Disable

Emergency disable **不是普通 rollback**。它同时把 release route 置为 `disabled` 并关闭 candidate deployment，因而新 Admission fail-closed。对于已提交的 Provider work，既有 Supply Broker 的 `PrepareControl` 明确允许 disabled deployment 继续 status/cancel/reconciliation；这不会重新开放新 submission。

随后显式 rollback 可以恢复 prior stable 的新流量，但不会自动把 emergency-disabled candidate 改回 active，事故历史也保留在 append-only release audit 中。

## 权限与隔离

- Release 管理仅允许 Workspace `owner/admin`；Developer 只有 Release read，没有 Release manage；
- Admin API 使用 Browser Session + CSRF；客户端不能提交 actor、state、revision；所有 mutation 必须有 reason；
- 独立 `release-manager` PostgreSQL role 只能 SELECT Release facts 并 EXECUTE 受控函数；不能直接写 Release 表、Deployment、Catalog、Distribution、Execution；
- Runtime role 只能执行 `supply.resolve_release_route`，不能 SELECT Release tables 或 Deployment；
- ReleasePlan、route 与 audit 都启用 `FORCE ROW LEVEL SECURITY`；audit append-only；
- candidate 在 canary/promote 前必须有 active/provider-compatible Deployment、published ToolVersion/Toolset binding，以及 published PluginVersion supply evidence。

## 不属于 S4-B 完成声明

以下仍为 deferred，不能因为 T23 已通过而描述为已实现或已认证：

- 百分比 weighted/random canary 与自动指标 promotion；
- Admin / Publisher Release 前端工作台；
- 通用危险操作 JIT 授权与支持会话；
- 商业账务、调账、退款、支付供应商；
- CLI / Skill 分发；
- 制品签名、依赖扫描与完整供应链认证；
- production deploy、容量、灾备、RPO/RTO、on-call 与 G4/G5 上线认证。

S4-B 完成的是**受控发布路由与事故开关的数据面/控制面合同**，不是 production-ready 声明。

## 下一工作包

下一工作包进入 **S4-C dangerous-operation / JIT governance**：把现有局部 maker/checker 与 Release Governance 进一步抽象到危险操作审批、消费标记、理由与临时授权；商业账务与支付仍保持独立工作包和独立准入证据。

