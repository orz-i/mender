# Upstream MCP Client Adapter Foundation

本阶段把远程 MCP Server 作为 Mender 的一种 Supply 来源接入，但继续保持“协议适配在外层、平台权限/预算/Run 不旁路”的边界。官方 SDK 固定 `github.com/modelcontextprotocol/go-sdk v1.7.0`，首版只接受 MCP `2026-07-28` stateless Streamable HTTP 的 Tools 能力。

## 第一切片：合同与持久化基础

- `0019_upstream_mcp_contracts.sql` 为 `supply.deployments` 增加 `mcp_streamable_http`、固定协议版本和 stateless 标记；HTTP deployment 仍保持原有幂等 header 合同。
- MCP deployment 不使用通用 `Idempotency-Key`：当前协议没有可假设所有上游都支持的业务幂等 header。未来 `tools/call` 对副作用工具不得因网络不确定性盲目自动重试。
- endpoint 仍是审核后的 Deployment 固定事实，模型参数、Tool arguments 和上游 description 都不能覆盖 scheme/host/port/path。
- 上游认证复用 Connection → SecretProvider/Broker；平台 machine API Key 不允许直接透传给远程 MCP Server。
- `supply.mcp_tool_snapshots` 记录 append-only discovery snapshot：tool name、title/description、input/output Schema、annotations、内容摘要和发现时间。上游描述与 annotations 全部按不可信输入处理。
- Schema 变化创建新的 snapshot；发现结果**不会自动覆盖或发布** Catalog 的 immutable ToolVersion。

## 尚未完成

本切片尚未开启远程网络调用，也没有实现 `server/discover`、`tools/list`、`tools/call`、OAuth、Resources、Prompts、Tasks、MRTR、Agent/A2A。第二切片才会建立受控 outbound MCP Client adapter，并复用现有 egress/secret 边界。

