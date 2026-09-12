# Session：Mender Upstream MCP Client Adapter Foundation

**Session id:** ses_fb8bf7db7ed14e029a0773cf6e00fdae
**Created:** unix:1789132507
**Updated:** unix:1789194432
**Status:** completed
**Host session scope:** host-session:043ee4df576639eee2b229a35ac724174bbb1140023cfc08278b9ea5b952880b
**Parent session id:** ses_0522c54fe991488e95259e466cb638d1

## 用户核心目标

- 进入 Mender 下一阶段：实现 Upstream MCP Client Adapter Foundation。在已经完成的 Fixed Toolset MCP Distribution、Supply HTTP Runtime、Connections、Admission、Execution、Artifact 与 machine-key 鉴权基础上，引入远程 MCP Server 作为能力来源，但继续把 MCP SDK 限制在 adapter 层。分三段提交：1) 扩展 Supply/Connection/Deployment 合同，增加受控 remote_mcp endpoint、协议版本、认证方式与 capability snapshot，要求固定 endpoint、HTTPS/SSRF 约束和 secret broker，不把平台 bearer token 透传上游；2) 建立 outbound MCP Client adapter，使用官方 github.com/modelcontextprotocol/go-sdk v1.7.0 对经审核的远程 MCP Server 执行 server/discover、tools/list、tools/call，并把上游 Tool schema/annotations 映射为候选 Catalog ToolVersion，不宣称透明支持 Resources/Prompts/Tasks/OAuth；3) 增加本地 loopback MCP Server + 真实 PostgreSQL E2E，验证 discovery/import/call、schema drift、disabled deployment、credential isolation、upstream error/cancel/timeout、tenant isolation、no token passthrough，并通过 pnpm check、go test -race、git diff --check。

## 已确认事实


## 已完成修改


## 关键设计决定


## 测试结果


## 当前运行状态


## 剩余问题


## 下一步


## 本轮检查点

### auto-8dd162f6bcd3af09

```json
{
  "turn_id": "auto-8dd162f6bcd3af09",
  "timestamp": "unix:1789132914",
  "user_intent": "",
  "findings": [],
  "decisions": [],
  "files_changed": [],
  "tests": [],
  "runtime_state": [],
  "remaining_issues": [],
  "next_actions": [],
  "notes": ""
}
```

### close-work-session-04e23a3c4f0f4d168d20bde1630f5153

```json
{
  "turn_id": "close-work-session-04e23a3c4f0f4d168d20bde1630f5153",
  "timestamp": "unix:1789194432",
  "user_intent": "进入 Mender 下一阶段：实现 Upstream MCP Client Adapter Foundation。在已经完成的 Fixed Toolset MCP Distribution、Supply HTTP Runtime、Connections、Admission、Execution、Artifact 与 machine-key 鉴权基础上，引入远程 MCP Server 作为能力来源，但继续把 MCP SDK 限制在 adapter 层。分三段提交：1) 扩展 Supply/Connection/Deployment 合同，增加受控 remote_mcp endpoint、协议版本、认证方式与 capability snapshot，要求固定 endpoint、HTTPS/SSRF 约束和 secret broker，不把平台 bearer token 透传上游；2) 建立 outbound MCP Client adapter，使用官方 github.com/modelcontextprotocol/go-sdk v1.7.0 对经审核的远程 MCP Server 执行 server/discover、tools/list、tools/call，并把上游 Tool schema/annotations 映射为候选 Catalog ToolVersion，不宣称透明支持 Resources/Prompts/Tasks/OAuth；3) 增加本地 loopback MCP Server + 真实 PostgreSQL E2E，验证 discovery/import/call、schema drift、disabled deployment、credential isolation、upstream error/cancel/timeout、tenant isolation、no token passthrough，并通过 pnpm check、go test -race、git diff --check。",
  "findings": [],
  "decisions": [],
  "files_changed": [],
  "tests": [],
  "runtime_state": [],
  "remaining_issues": [],
  "next_actions": [],
  "notes": "完成 Upstream MCP Client Adapter Foundation 三个切片。已建立 reviewed remote MCP Deployment/Connection/discovery snapshot 合同、官方 MCP Go SDK v1.7.0 的 2026-07-28 stateless Streamable HTTP tools/list/tools/call outbound adapter、精确 snapshot hash 路由、独立 mcp-connector 最小数据库角色、Connection→SecretProvider 凭据注入、结果 evidence → ProviderObservation → Artifact/Settlement 收敛。真实 Windows/PostgreSQL E2E 期间修复了三项问题：私有 pinned HTTP transport 强制 Connection: close 导致 Windows loopback WSAECONNRESET，改为私有 keepalive 并保持 proxy 禁用/DNS pinning/HTTP2 禁用/显式 CloseIdleConnections；Upstream E2E 使用共享 ws_a 导致 Worker 可能租到前序 queued Job，改为独立 ws_upstream_mcp；同步 tools/call 结果时间可能早于 Dispatcher durable submitted evidence，ProviderTarget 新增稳定 EvidenceAt 并在 reconciliation 中用持久 evidence 作为 observation 时间下界，不放宽 ProviderResults invariant。最终 pnpm test:integration:docker、pnpm check、go test -race -count=1 ./...、git diff --check 全部通过，HTTP cancel 与 Upstream MCP Windows loopback 压力测试分别 100 次通过。"
}
```

