# G3 Compatibility & Entry Matrix Alpha

日期：2026-09-14  
机器可验证清单：[g3-evidence.json](g3-evidence.json)

## 结论边界

本证据包只认证 Mender 当前 HTTP MCP surface 的 **2026-07-28** 协议版本，以及仓库锁定的 `github.com/modelcontextprotocol/go-sdk` **v1.7.0** automated harness。认证范围包含动态 Meta-tool endpoint 与 Fixed Toolset endpoint 两种分发模式。这里的 “certified” 表示仓库自动化测试通过，不表示所有实现同一协议的第三方客户端都已经兼容验证。

选定旧版 **2025-11-25** 在本阶段是明确的 rejected 组合，不是兼容版本。服务端要求 `Mcp-Protocol-Version: 2026-07-28`，并在认证和 SDK 之前检查 initialize body 中的 `protocolVersion`；current header 不能掩盖 legacy initialize body，legacy header 也不能通过 current body 绕过入口边界。协议 header 或 initialize version 不匹配不会进入身份认证，更不会进入 Admission。

当前没有 third-party MCP client 被本证据包认证。官方 Go SDK v1.7.0 只是 automated harness，不可外推为 Claude、Cursor、其他 MCP client 或旧协议版本的兼容声明。A2A、multi-turn Agent supplemental input、payment accounting 与 production proxy 同样 not certified。

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

## 需求与证据映射

- **S3-13 / T13–T16 / T40**：`backend/tests/mcp/protocol_test.go`、`backend/tests/mcp/compatibility_matrix_test.go`、`backend/tests/mcp/fixed_toolset_test.go`。覆盖官方 SDK current-version 会话、两种分发、header/body 协议一致性、错误版本和认证边界。
- **S3-15**：`backend/tests/integration/g3_entry_matrix_test.go` 由 `TestPostgresRuntimeContract` 的 `G3 same-subject three-source Entry Run Artifact convergence matrix` 子测试执行，覆盖 same Workspace/subject 的 HTTP、upstream MCP、Remote Agent 三来源闭环。
- **S3-18**：本文件和 `g3-evidence.json` 是当前 supported-client / limit 证据。当前 supported harness 只有 Go SDK v1.7.0；third-party client 列表为空。

## 不在本证据包中的声明

本阶段没有增加 2025-11-25 compatibility，没有 A2A adapter、多轮 Agent resume、任意脚本／表达式运行时、生产代理或公网兼容实验，也没有 payment accounting。已有 Provider Callback、Artifact Object Lifecycle、OAuth 与其他 G3 相邻能力继续由各自 integration/engineering 证据覆盖，本文件不把它们重新命名为新的兼容承诺。
