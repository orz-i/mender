# G3 Compatibility & Entry Matrix Alpha

日期：2026-09-14  
机器可验证清单：[g3-evidence.json](g3-evidence.json)

## 结论边界

本证据包只认证 Mender 当前 HTTP MCP surface 的 **2026-07-28** 协议版本，以及仓库锁定的 `github.com/modelcontextprotocol/go-sdk` **v1.7.0** automated harness。认证范围包含动态 Meta-tool endpoint 与 Fixed Toolset endpoint 两种分发模式。这里的 “certified” 表示仓库自动化测试通过，不表示所有实现同一协议的第三方客户端都已经兼容验证。

选定旧版 **2025-11-25** 在本阶段是明确的 rejected 组合，不是兼容版本。服务端要求 `Mcp-Protocol-Version: 2026-07-28`，并在认证和 SDK 之前检查 initialize body 中的 `protocolVersion`；current header 不能掩盖 legacy initialize body，legacy header 也不能通过 current body 绕过入口边界。协议 header 或 initialize version 不匹配不会进入身份认证，更不会进入 Admission。

当前没有 third-party MCP client 被本证据包认证。官方 Go SDK v1.7.0 只是 automated harness，不可外推为 Claude、Cursor、其他 MCP client 或旧协议版本的兼容声明。Remote Agent 的 **T17 one-shot supplemental input** 已有仓库自动化证据，但 A2A、multi-turn Agent resume / conversation、payment accounting 与 production proxy 仍然 not certified。

## MCP 协议与分发矩阵

| 维度 | 组合 | 本阶段状态 | 可执行证据 |
| --- | --- | --- | --- |
| 协议 | 2026-07-28 | certified | `TestStatelessOfficialSDKNegotiates20260728AfterMenderAuthentication` |
| 协议 | 2025-11-25 | rejected | `TestCertifiedMCPDistributionCompatibilityMatrix` |
| 动态分发 | `/mcp/v1/workspaces/{workspace_id}` | certified | official SDK session + Meta-tool tests |
| 固定分发 | `/mcp/v1/workspaces/{workspace_id}/toolsets/{toolset_version_id}` | certified | Fixed Toolset direct-tool tests + shared compatibility matrix |
| 降级／错配 | current header + legacy initialize body | rejected before auth | shared initialize protocol guard |
| 鉴权 | current protocol + invalid Bearer | 401 after protocol checks | shared compatibility matrix |

协议入口仍由官方 SDK 处理 JSON-RPC、initialize 与工具调用语义。Mender 新增的 pre-SDK guard 只对可解析的 initialize `protocolVersion` 做 bounded 一致性检查，并将 request body 原样恢复；它不是第二套 MCP parser。

## 同一主体三来源 Entry → Run → Artifact

真实 PostgreSQL integration 使用固定测试身份 `ws_g3_matrix / sa_g3_matrix` 和同一个 Toolset `set_g3_matrix_v1`，在同一 isolated database 内依次走三种来源：

| 来源 | reviewed transport | 入口与执行 | 收敛事实 |
| --- | --- | --- | --- |
| API as Tool | `http` | protected StartRun → Worker → reviewed HTTP executor → Provider status | succeeded Run、finished Job、provider_result Artifact、settlement |
| MCP Tool | `mcp_streamable_http` | protected StartRun → Worker → pinned upstream MCP snapshot/call → reconciler | succeeded Run、finished Job、provider_result Artifact、settlement |
| Agent as Tool | `agent_http` | protected StartRun → Worker → reviewed Agent submit/status → Provider control | succeeded Run、finished Job、provider_result Artifact、settlement |

三条路径使用相同的 `run:create` subject、Admission、Governance policy、Run/Job/Attempt 状态机、Artifact 和 Commerce settlement。每次受理都先做事务前 Governance evaluate，再在 Admission UoW 内重新 enforce，因此每个 ToolVersion 有两个 allow decision；矩阵验证三个 adapter 的阶段集合相同，而不是错误地假设“一次 Run 只有一条治理审计记录”。

成功后每个 Run 都只产生一个 `provider_result` Artifact 和一个 terminal settlement handoff。测试中的三种价格均为 10 micro；三条成功路径最终在同一个 BudgetPeriod 中收敛到 `consumed_micro=30`、`reserved_micro=0`。这只是既有 quota/usage settlement 事实，不代表支付、钱包、供应商分账或收入确认。

Provider ID、provider request ID、external task ID 与 mounted supplier secret 只存在于受控内部事实。受保护 Run/Artifact API 再读同一三个结果时不得出现这些 adapter-specific handles；另一个 Workspace 的凭据读取 `ws_g3_matrix` Run 必须返回 403。

## T17 Remote Agent one-shot supplemental input Alpha

T17 现在补齐 reviewed `agent_http` 的提交、轮询、取消、重复 callback、取消未确认、Artifact 以及 **one-shot supplemental input** 合同。Provider status 或已验签 `provider.input_required` callback 只能在既有 Attempt 的 `workspace/run/attempt/provider_id/provider_request_id/external_task_id` 精确绑定下创建 input request；服务端把 Run 投影为 `waiting_input`，只持久化 bounded prompt、JSON Schema、request ID 与时间元数据，不把 Provider credential、endpoint 或 raw answer 写入普通 Run/Artifact/Governance projection。

调用方必须持有显式 `run:input` scope。Machine Key 和短期 Human Run delegation 都重新经过服务端授权；`run:read` 或 `run:cancel` 不隐式授予输入权限。answer 必须是 ≤64 KiB JSON object，并通过服务端保存的 Provider schema；Execution 数据库只记录 SHA-256 answer digest 与 deterministic submission ID。合法发送通过固定 reviewed Agent input endpoint，沿用现有 mounted Secret、host allowlist 与 SSRF 边界，浏览器不能提交 Provider URL 或 handle。

真实 isolated PostgreSQL + local Agent server 覆盖 accepted 与 unknown 两条路径：accepted 后 `waiting_input → running`，相同 request + answer replay 不重复网络副作用，不同 answer 冲突；若 Provider 在可能收到 answer 后返回不确定结果，则 input request 进入 `unknown`、Run 进入 `reconciling`，相同 answer 再提交仍 **unknown no-resend**。成功后继续通过既有 ProviderObservation → `provider_result` Artifact 收敛。当前 Alpha 对每个 Run 最多允许一个 supplemental input request，因此这里不认证 multi-turn Agent conversation、任意 resume loop 或 A2A。

## T28 Trusted Provider Callback / Inbox Alpha

T28 的 callback ingress 只认证 reviewed Provider 的固定入口，不是用户可配置 Webhook。HTTP adapter 必须先对**原始 body**执行 **raw-body HMAC** 验证，再做严格 JSON 解析；timestamp、key-id 和签名失败、重复字段或篡改都不会进入 receiver。真实 PostgreSQL Inbox 按 Provider event identity 记录安全元数据；相同 event/body 重放只增加 delivery count，不重复 ProviderObservation 或 Artifact。

不同 body 复用相同 event ID 会形成 **event-id conflict** quarantine；已终态 Run 的重复结果进入 `terminal_replay` quarantine；Attempt 的 provider request / external task 绑定不一致进入 `attempt_binding_mismatch`；比最新已接受 observation 更旧的 callback 按 **out-of-order** quarantine，均不得推进 Run。callback-ingestor role 受 FORCE RLS 约束，显式跨 Workspace Inbox INSERT 被真实 PostgreSQL 拒绝；独立 callback-observer 只读 provider/event/disposition/delivery 等安全投影，不能读取 body digest、key id、原始 payload 或 Execution 敏感字段。

本证据只说明本地 reviewed HMAC/Inbox/Run convergence 的 Alpha 合同成立，不认证第三方 Provider 的公网 webhook SLA、消息总线、用户自定义 callback URL 或生产告警通道。

## T30 Artifact Object Lifecycle Alpha

T30 保留 immutable inline `provider_result` Artifact 为兼容真源，并对 >256 KiB 的受支持 JSON 结果建立 reviewed filesystem sidecar。对象 key 只由服务端确定性生成；独立 materializer role 与 metadata-reader role 均为最小权限。对象状态、签名 URL 和 Console 投影都不暴露 object key 或 filesystem root。

短时读取必须先重新执行 `run:read`，再签发 **short-lived capability**；signed GET 再校验 Workspace/Run/Artifact、生命周期、SHA-256 与 size。真实 integration 证明跨 Workspace credential 不能签发 capability、匿名签发返回 401、capability 篡改失败。到期 cleanup 的顺序是 **physical delete** 后单向标记 expired；expired 状态停止签发，旧 capability 返回 410 Gone。

该证据只认证 reviewed local filesystem sidecar 的 Alpha 生命周期，不认证 S3/MinIO/cloud bucket、CDN、公共对象 URL、任意文件上传或生产级对象存储运维。

## T07 / T08 Reviewed OAuth Refresh Alpha

G3 证据现在补充 **T07 / T08** 的自动化 OAuth refresh 语义，但这里仍然是 `covered-alpha`，不是某个第三方 OAuth Provider 的生产认证。初始授权只有在 `MENDER_CONNECTION_OAUTH_REFRESH_ENABLED=true` 时才要求并保存 refresh token；后台刷新还必须单独启用 `MENDER_REVIEWED_OAUTH_REFRESH_ENABLED=true`，并提供独立 `oauth-refresher` 数据库角色。两个开关默认都是 false。

Access token 与 refresh token 始终只进入 mounted Secret Vault。PostgreSQL 只保存 opaque credential ref、Connection/refresh revision、required/granted scope、时间与状态；浏览器投影、日志和 G3 manifest 都没有 token bytes。Access 与 refresh 使用不同 immutable vault address，后台刷新只根据当前 Connection revision 与 sidecar revision 读取当前 refresh secret。

真实 isolated PostgreSQL + Secret Vault evidence 覆盖：两个并发 refresh 都可以到达 reviewed token endpoint，但 Connection revision CAS 只有一个 winner；loser 写入的新 secret 会被清理，winner 切换到新的 immutable access ref。Provider 返回新 refresh token 时按新 ref/revision 轮换；未返回 refresh token 时继续使用已有 refresh secret。返回 scope 时必须继续覆盖 required scope；scope 缩减会将 Connection/refresh sidecar 收敛到 `error / scope_reduced`。OAuth `invalid_grant` 同样进入 durable error；5xx / 网络类瞬时错误保持 Connection `active`，供后续 bounded cycle 重试。

Connection revoke 会同步把 refresh sidecar 置为 revoked；被撤销 Connection 不再是 refresh candidate。`oauth-refresher` principal 只读必要 Connection / refresh metadata 和非秘密 grant expiry 字段，不能读取 grant subject、Identity、Execution、Commerce 或 Supply，也不能 INSERT refresh sidecar。普通 browser-side `connection-manager` 仍不能 SELECT refresh metadata；它只通过窄的 Workspace/revision 绑定函数同步 revoke。

本 evidence 不认证 Provider 应用审核、第三方 OAuth provider 的生产行为、token introspection、DPoP、OIDC user-session refresh 或任意自定义 OAuth 脚本。T07 / T08 在这里表示仓库自动化已覆盖刷新竞争、rotation/no-rotation、scope shrink、invalid_grant、transient retry、revoke 和 token isolation，不表示外部 Provider 已完成商业/生产验收。

## 需求与证据映射

- **S3-14 / T07 / T08**：`backend/tests/connections/oauth_http_test.go`、`backend/internal/contexts/connections/adapters/outbound/oauth/client_test.go`、`backend/tests/integration/oauth_refresh_runtime_test.go`。覆盖初始 refresh secret capture、reviewed token endpoint、并发 CAS、rotation/no-rotation、scope shrink、invalid_grant、瞬时重试、revoke、Secret Vault 隔离与最小权限。
- **S3-07 / S3-11 / S3-14 / T17**：`backend/tests/integration/remote_agent_runtime_test.go`、`backend/internal/contexts/supply/adapters/outbound/http/executor_agent_test.go`、`backend/internal/processes/providercallback/adapters/inbound/httpapi/handler_test.go` 与 protected Run input API/client。覆盖 reviewed Agent submit/status/cancel、signed input callback、`run:input`、schema validation、digest-only persistence、idempotent replay、unknown no-resend、waiting_input resume 与 Artifact 收敛。
- **S3-04 / S3-14 / T28**：`backend/internal/processes/providercallback/adapters/inbound/httpapi/handler_test.go` 与 `backend/tests/integration/provider_callback_runtime_test.go`。覆盖 raw-body HMAC、tamper/stale signature、duplicate delivery、event-id conflict、Attempt binding、out-of-order/terminal replay quarantine、Workspace RLS、least-privilege observer 与 Run/Artifact convergence。
- **S3-03 / S3-14 / S3-17 / T30**：`backend/internal/contexts/execution/application/artifact_objects_test.go` 与 `backend/tests/integration/artifact_result_test.go`。覆盖 object materialization、run:read、short-lived capability、digest/size 重验、跨 Workspace deny、physical delete、expiry 与旧 capability Gone。
- **S3-13 / T13–T16 / T40**：`backend/tests/mcp/protocol_test.go`、`backend/tests/mcp/compatibility_matrix_test.go`、`backend/tests/mcp/fixed_toolset_test.go`。覆盖官方 SDK current-version 会话、两种分发、header/body 协议一致性、错误版本和认证边界。
- **S3-15**：`backend/tests/integration/g3_entry_matrix_test.go` 由 `TestPostgresRuntimeContract` 的 `G3 same-subject three-source Entry Run Artifact convergence matrix` 子测试执行，覆盖 same Workspace/subject 的 HTTP、upstream MCP、Remote Agent 三来源闭环。
- **S3-18**：本文件和 `g3-evidence.json` 是当前 supported-client / limit 证据。当前 supported harness 只有 Go SDK v1.7.0；third-party client 列表为空。

## 不在本证据包中的声明

本阶段没有增加 2025-11-25 compatibility，没有 A2A adapter、多轮 Agent resume / conversation、任意脚本／表达式运行时、生产代理或公网兼容实验，也没有 payment accounting。T17 只认证上述 one-shot supplemental input；T28 只认证 reviewed signed Inbox；T30 只认证 reviewed filesystem sidecar lifecycle；OAuth refresh 只覆盖上述 T07/T08 自动化语义。这些 Alpha 证据都不能扩写成第三方 Provider、云对象存储或生产网络设施已经完成认证。
