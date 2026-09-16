# S4-C Dangerous Operation / JIT Governance

日期：2026-09-16  
机器证据：[s4c-dangerous-operation-jit-evidence.json](s4c-dangerous-operation-jit-evidence.json)

## 结论

**S4-C Dangerous Operation / JIT Governance｜T24／T26：COMPLETE**。

本工作包把高风险操作从普通“有权限即可执行”升级为持久化 maker/checker 审批，并实现独立于租户成员关系的 Platform Staff + JIT 支持访问。它不是通用 impersonation 系统，也不是支付／退款审批系统。

## T24：精确绑定的一次性危险操作审批

当前受控动作只包含 `release.emergency_disable` 与 `support.workspace_read`。审批绑定 requester/subject、action、target kind/id/version、规范化参数 SHA-256、可选 exact amount/currency 以及 expiry；这些字段一经创建不可改写。

审批流程为 `pending → approved/rejected/expired → consumed`。请求者不能自审；消费只能发生一次。浏览器不能通过 `approved=true`、revision、actor、参数摘要或类似字段制造审批事实。

Release emergency-disable 已成为第一个真实消费方：申请函数由数据库读取当前 ReleasePlan revision，并固定 emergency 参数；Release mutation 必须携带已批准的 `approval_id`，并在同一事务中消费审批。目标 revision 改变、审批过期、consumer 不是 requester、审批已消费或参数不匹配均拒绝。审批消费后若 Release CAS／deployment 变更失败，整个事务一起回滚。

合同已保留 optional amount/currency 精确绑定字段，但 **S4-C 没有实现任何 payment/refund/financial approval consumer**，不能据此宣称商业账务审批已经完成。

## T26：Platform Staff 与 JIT Support

`identity.platform_staff` 是全局平台身份事实，角色限定为 support / reviewer / operator / auditor。它不创建、修改或替代 `workspace_memberships`；Platform Staff 身份本身不赋予 `run:read`、`release:manage` 等租户权限。

Support JIT 申请只能使用 `workspace:read`、`run:read`、`usage:read` 三个只读 scope，grant TTL 为 5–60 分钟。support/operator 可以申请；reviewer/operator 负责独立审核。激活时只提交 approval id，不允许重新绑定 scopes 或 TTL；到期与撤销均由数据库真源实时执行。

当前真正暴露的 JIT 数据面只有安全 Run 元数据：`id / state / version / created_at / updated_at`。专用 `support-reader` **没有 execution tenant 表的直接 SELECT 权限，也不能读取 JIT grant 表或 identity 表**；它唯一的 tenant 数据入口是 `governance.list_jit_support_runs(...)` SECURITY DEFINER 投影，该函数每次读取前重新验证 workspace、Platform Staff、`run:read` scope、grant 生效时间、到期和撤销状态。

因此 JIT access 不等于秘密访问、Connection/credential 读取、Artifact/Event/Admission 参数读取，也不等于模拟某个租户用户。

## 权限与数据库边界

S4-C 使用三个相互分离的 restricted role：

- `dangerous-operation-manager`：读审批/审计事实并调用受控申请、review、JIT activate/revoke wrapper；不能调用 generic submit，不能消费 Release approval，不能访问 supply/commerce/identity 原表；
- `release-manager`：执行 Release lifecycle；emergency-disable 必须消费治理审批，但它不能申请或批准审批；
- `support-reader`：不能直接读取 tenant/identity/governance grant 表，只能调用 JIT-gated safe Run projection。

Workspace-scoped approval、audit 与 JIT grant 均 FORCE RLS。Dangerous/JIT mutation API 使用 Human Browser Session + CSRF；Platform Staff 的平台角色通过独立 identity lookup 授权，而不是借 Workspace membership。

## 真实 PostgreSQL 演练

`backend/tests/integration/dangerous_operation_jit_test.go` 使用真实隔离 PostgreSQL 验证：

- release-manager 与 dangerous-operation-manager 双向越权失败；
- generic dangerous submit 不能被 restricted manager 用来伪造参数；
- requester 自审被拒绝；
- Release revision 漂移后旧审批不可消费且仍保留 approved 事实；
- 已批准但过期的审批不可消费；
- 错误 requester 不可消费；
- 成功消费后不能重放；
- Platform Staff 在整个 JIT 生命周期中都没有 Workspace membership；
- mutation scope 请求被拒绝；
- 未激活、过期、撤销后的 JIT Run 读取都由数据库拒绝；
- support-reader 无法直接读取 Run、Admission、Event、Artifact、JIT grant 或 Platform Staff 表；
- JIT audit 的 requested / approved / consumed / granted / revoked 事件完整保留。

## 明确 deferred

S4-C 不代表危险操作框架已经覆盖所有平台动作，也不代表通用 Support impersonation、支付／退款、商业账务、秘密/credential 支持访问、MFA/生产身份保证、前端 JIT 工作台或 production deploy 已完成。

下一工作包为 **S4-D Admin Operations（对应计划 S4-05）**：继续处理 Workspace freeze、异常处理、受控 Admin 操作与审计导出，而不是扩大 JIT 数据权限。
