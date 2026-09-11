# MCP Meta-Tool Gateway Foundation

阶段状态：实现中。目标是把 Mender 已有的 Admission、Run、Artifact 能力通过官方 MCP Go SDK 暴露为受认证的平台元工具，而不是提前宣称任意 Toolset 已能直接作为 MCP Tools 发布。

协议基线固定为官方 `github.com/modelcontextprotocol/go-sdk` v1.7.0；该稳定版本实现 MCP `2026-07-28`。Mender 首个 HTTP 入口只认证并测试 `2026-07-28`，使用 stateless Streamable HTTP。SDK 同时保留旧协议兼容代码，但本阶段不把旧版本列入 Mender 的兼容承诺。

首个入口为 `/mcp/v1/workspaces/{workspace_id}`。HTTP Bearer machine Key 在进入 SDK handler 前认证，并与路径 Workspace 交叉验证。MCP request/session 身份不作为 Mender 授权凭据。后续元工具继续走现有 `run:create`、`run:read`、`run:cancel` 和 Admission 预算／Connection／Toolset 规则。

DDD 边界：MCP SDK 仅允许出现在 `processes/mcpbridge/adapters/inbound`；mcpbridge application 使用消费方端口；跨 identity、admission、execution 均由 outbound ACL 依赖对方 public 合同。bootstrap 只负责显式装配。

本阶段不包括固定 Toolset direct tools、上游 MCP Client、OAuth、Resources／Prompts、MRTR、Agent/A2A、对象存储或支付。固定 Toolset direct tools 需要后续补齐 Catalog 的正式输入 Schema／描述合同和 Toolset Connection／费用策略快照后再实现。
