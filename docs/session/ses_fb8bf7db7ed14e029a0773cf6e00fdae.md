# Session：Mender Upstream MCP Client Adapter Foundation

**Session id:** ses_fb8bf7db7ed14e029a0773cf6e00fdae
**Created:** unix:1789132507
**Updated:** unix:1789132876
**Status:** active
**Host session scope:** host-session:043ee4df576639eee2b229a35ac724174bbb1140023cfc08278b9ea5b952880b
**Parent session id:** ses_0522c54fe991488e95259e466cb638d1

## 用户核心目标

- 进入 Mender 下一阶段：实现 Upstream MCP Client Adapter Foundation。在已经完成的 Fixed Toolset MCP Distribution、Supply HTTP Runtime、Connections、Admission、Execution、Artifact 与 machine-key 鉴权基础上，引入远程 MCP Server 作为能力来源，但继续把 MCP SDK 限制在 adapter 层。分三段提交：1) 扩展 Supply/Connection/Deployment 合同，增加受控 remote_mcp endpoint、协议版本、认证方式与 capability snapshot，要求固定 endpoint、HTTPS/SSRF 约束和 secret broker，不把平台 bearer token 透传上游；2) 建立 outbound MCP Client adapter，使用官方 github.com/modelcontextprotocol/go-sdk v1.7.0 对经审核的远程 MCP Server 执行 server/discover、tools/list、tools/call，并把上游 Tool schema/annotations 映射为候选 Catalog ToolVersion，不宣称透明支持 Resources/Prompts/Tasks/OAuth；3) 增加本地 loopback MCP Server + 真实 PostgreSQL E2E，验证 discovery/import/call、schema drift、disabled deployment、credential isolation、upstream error/cancel/timeout、tenant isolation、no token passthrough，并通过 pnpm check、go test -race、git diff --check。

## 已确认事实

- 自动阶段检查点：tool=apply_patch, status=completed, success=true
- summary=A backend/tests/supply/mcp_contract_test.go
A contracts/upstream-mcp-contract.json
M contracts/README.md
M scripts/check-contracts.mjs
A docs/engineering/2026-09-11-upstream-mcp-client-foundation.md
- 自动阶段检查点：tool=exec_command, status=succeeded, success=true
- command=pnpm check:contracts

## 已完成修改

- backend/tests/supply/mcp_contract_test.go
- contracts/upstream-mcp-contract.json
- contracts/README.md
- scripts/check-contracts.mjs
- docs/engineering/2026-09-11-upstream-mcp-client-foundation.md

## 关键设计决定


## 测试结果

- verification_kind=test, success=true

## 当前运行状态

- task_id=04e23a3c4f0f4d168d20bde1630f5153
- task_status=active
- tool=exec_command
- session_id="48b6f255-1fa3-4a70-ac0d-639a0d10d816"
- execution_status="succeeded"
- exit_code=0
- last_output_at="2026-09-11T13:21:15.173Z"
- branch=main
- head=1209df2f6fe2dceb5e0f2d2ec3c646f3a2ee85cd
- baseline_matches=Some(true)

## 剩余问题


## 下一步


## 本轮检查点

### auto-apply_patch-db5b4810c80c63df

```json
{
  "turn_id": "auto-apply_patch-db5b4810c80c63df",
  "timestamp": "unix:1789132821",
  "user_intent": "进入 Mender 下一阶段：实现 Upstream MCP Client Adapter Foundation。在已经完成的 Fixed Toolset MCP Distribution、Supply HTTP Runtime、Connections、Admission、Execution、Artifact 与 machine-key 鉴权基础上，引入远程 MCP Server 作为能力来源，但继续把 MCP SDK 限制在 adapter 层。分三段提交：1) 扩展 Supply/Connection/Deployment 合同，增加受控 remote_mcp endpoint、协议版本、认证方式与 capability snapshot，要求固定 endpoint、HTTPS/SSRF 约束和 secret broker，不把平台 bearer token 透传上游；2) 建立 outbound MCP Client adapter，使用官方 github.com/modelcontextprotocol/go-sdk v1.7.0 对经审核的远程 MCP Server 执行 server/discover、tools/list、tools/call，并把上游 Tool schema/annotations 映射为候选 Catalog ToolVersion，不宣称透明支持 Resources/Prompts/Tasks/OAuth；3) 增加本地 loopback MCP Server + 真实 PostgreSQL E2E，验证 discovery/import/call、schema drift、disabled deployment、credential isolation、upstream error/cancel/timeout、tenant isolation、no token passthrough，并通过 pnpm check、go test -race、git diff --check。",
  "findings": [
    "自动阶段检查点：tool=apply_patch, status=completed, success=true",
    "summary=A backend/tests/supply/mcp_contract_test.go\nA contracts/upstream-mcp-contract.json\nM contracts/README.md\nM scripts/check-contracts.mjs\nA docs/engineering/2026-09-11-upstream-mcp-client-foundation.md"
  ],
  "decisions": [],
  "files_changed": [
    "backend/tests/supply/mcp_contract_test.go",
    "contracts/upstream-mcp-contract.json",
    "contracts/README.md",
    "scripts/check-contracts.mjs",
    "docs/engineering/2026-09-11-upstream-mcp-client-foundation.md"
  ],
  "tests": [],
  "runtime_state": [
    "task_id=04e23a3c4f0f4d168d20bde1630f5153",
    "task_status=active",
    "tool=apply_patch",
    "branch=main",
    "head=1209df2f6fe2dceb5e0f2d2ec3c646f3a2ee85cd",
    "baseline_matches=Some(true)"
  ],
  "remaining_issues": [],
  "next_actions": [],
  "notes": "Anchor 自动保存的结构化阶段检查点；相同阶段身份会幂等更新。"
}
```

### auto-exec_command-249e4ff62b15d9f9

```json
{
  "turn_id": "auto-exec_command-249e4ff62b15d9f9",
  "timestamp": "unix:1789132876",
  "user_intent": "进入 Mender 下一阶段：实现 Upstream MCP Client Adapter Foundation。在已经完成的 Fixed Toolset MCP Distribution、Supply HTTP Runtime、Connections、Admission、Execution、Artifact 与 machine-key 鉴权基础上，引入远程 MCP Server 作为能力来源，但继续把 MCP SDK 限制在 adapter 层。分三段提交：1) 扩展 Supply/Connection/Deployment 合同，增加受控 remote_mcp endpoint、协议版本、认证方式与 capability snapshot，要求固定 endpoint、HTTPS/SSRF 约束和 secret broker，不把平台 bearer token 透传上游；2) 建立 outbound MCP Client adapter，使用官方 github.com/modelcontextprotocol/go-sdk v1.7.0 对经审核的远程 MCP Server 执行 server/discover、tools/list、tools/call，并把上游 Tool schema/annotations 映射为候选 Catalog ToolVersion，不宣称透明支持 Resources/Prompts/Tasks/OAuth；3) 增加本地 loopback MCP Server + 真实 PostgreSQL E2E，验证 discovery/import/call、schema drift、disabled deployment、credential isolation、upstream error/cancel/timeout、tenant isolation、no token passthrough，并通过 pnpm check、go test -race、git diff --check。",
  "findings": [
    "自动阶段检查点：tool=exec_command, status=succeeded, success=true",
    "command=pnpm check:contracts"
  ],
  "decisions": [],
  "files_changed": [],
  "tests": [
    "verification_kind=test, success=true"
  ],
  "runtime_state": [
    "task_id=04e23a3c4f0f4d168d20bde1630f5153",
    "task_status=active",
    "tool=exec_command",
    "session_id=\"48b6f255-1fa3-4a70-ac0d-639a0d10d816\"",
    "execution_status=\"succeeded\"",
    "exit_code=0",
    "last_output_at=\"2026-09-11T13:21:15.173Z\"",
    "branch=main",
    "head=1209df2f6fe2dceb5e0f2d2ec3c646f3a2ee85cd",
    "baseline_matches=Some(true)"
  ],
  "remaining_issues": [],
  "next_actions": [],
  "notes": "Anchor 自动保存的结构化阶段检查点；相同阶段身份会幂等更新。"
}
```

