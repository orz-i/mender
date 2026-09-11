# Fixed Toolset MCP Distribution Foundation

本阶段在已经完成的 MCP Meta-Tool Gateway 上增加固定 Toolset 的业务工具分发，不改变现有可靠执行内核。协议继续固定为官方 Go SDK v1.7.0 与 MCP 2026-07-28 stateless Streamable HTTP。

## 已实现边界

- Catalog ToolVersion 增加对 MCP 发布有意义的 title、description、input/output JSON Schema、side-effect、idempotency，并通过 `mcp_publishable` 显式 opt-in；旧 ToolVersion 默认不可直接发布。
- Distribution Binding 增加固定 `connection_id`、稳定 `mcp_name` 和 `mcp_exposed`；同 Workspace/Toolset 的公开名称唯一，旧 binding 默认隐藏。
- 固定入口：`/mcp/v1/workspaces/{workspace_id}/toolsets/{toolset_version_id}`。
- `tools/list` 每个请求重新读取 published bindings、ToolVersion 和当前 Connection grant；失效／撤销 Connection 不进入当前可调用面。
- 有效 Workspace 凭据如果缺少 `run:create`，仍可完成 MCP `server/discover`，但 `tools/list` 返回空业务工具面；这样权限不足不会被 SDK 误判成协议版本回退。真正 `tools/call` 会再次读取当前权限并 fail closed。
- `tools/call` 只接受业务参数和保留 `_mender` 控制块。Tool、版本、Toolset、Connection、Price、Budget、Deployment 都由服务端发布合同／Admission 解析，客户端不能覆盖。
- `_mender` 必须包含 `idempotency_key`、`currency`、`max_charge_micro`，进入 Admission 前从业务 arguments 中剥离。业务 arguments 使用已发布 input JSON Schema 做服务端验证，并保留原始 JSON bytes 进入 Admission，因此大整数不会因验证而改变持久化精度。
- 调用结果是异步 Run receipt：`run_id + submission_state=accepted + replayed + currency + reserved_micro`。Catalog 声明的业务 output schema 作为 MCP metadata 提供，只有后续 Provider succeeded Artifact 才是业务结果；本阶段不伪装同步业务结果。
- MCP Tool annotations 仅映射 read-only/idempotent 等提示，不参与授权或资金判断。
- MCP HTTP 断开只取消当前等待；已经持久受理的 Run 按既有 Admission 幂等语义恢复，同一逻辑重试必须复用 `_mender.idempotency_key`。

## 兼容与迁移

`0017_fixed_toolset_contracts.sql` 引入合同字段；其 checksum 不再修改。`0018_legacy_tool_contract_compat.sql` 单独放宽 legacy non-MCP ToolVersion 的 title 约束，保持现有 Meta-Tool/HTTP StartRun fixture 兼容，同时 MCP direct publish 仍要求完整合同和显式 opt-in。

## 未宣称

没有实现上游 MCP Client、OAuth、Resources/Prompts、MRTR、Agent/A2A、对象存储、支付或同步等待业务结果。本文件也不把正式 WBS/Gate 标为完成；真实 PostgreSQL E2E 与全仓门禁在本阶段第三切片记录。

