# Upstream MCP Client Adapter Foundation

本阶段把远程 MCP Server 作为 Mender 的一种 Supply 来源接入，但继续保持“协议适配在外层、平台权限/预算/Run 不旁路”的边界。官方 SDK 固定 `github.com/modelcontextprotocol/go-sdk v1.7.0`，首版只接受 MCP `2026-07-28` stateless Streamable HTTP 的 Tools 能力。

## 第一切片：合同与持久化基础

- `0019_upstream_mcp_contracts.sql` 为 `supply.deployments` 增加 `mcp_streamable_http`、固定协议版本和 stateless 标记；HTTP deployment 仍保持原有幂等 header 合同。
- MCP deployment 不使用通用 `Idempotency-Key`：当前协议没有可假设所有上游都支持的业务幂等 header。未来 `tools/call` 对副作用工具不得因网络不确定性盲目自动重试。
- endpoint 仍是审核后的 Deployment 固定事实，模型参数、Tool arguments 和上游 description 都不能覆盖 scheme/host/port/path。
- 上游认证复用 Connection → SecretProvider/Broker；平台 machine API Key 不允许直接透传给远程 MCP Server。
- `supply.mcp_tool_snapshots` 记录 append-only discovery snapshot：tool name、title/description、input/output Schema、annotations、内容摘要和发现时间。上游描述与 annotations 全部按不可信输入处理。
- Schema 变化创建新的 snapshot；发现结果**不会自动覆盖或发布** Catalog 的 immutable ToolVersion。

## 第二切片：受控 outbound MCP Client

- MCP SDK 只存在于 `supply/adapters/outbound/mcp`。Supply domain/application、Execution、Admission、Catalog 与 Connections 不依赖 SDK 类型。
- Client 使用固定 Deployment endpoint，重新执行 host allowlist、地址分类、DNS pinning、TLS、redirect/proxy 禁用与请求/响应大小限制；SDK 自身网络重试显式关闭。
- 上游认证只使用 Broker 从 Connection/SecretProvider 得到的受控 secret。Adapter 会移除任何已有 `Authorization` / `Proxy-Authorization`，再按 reviewed `none` / `bearer` / `header` 合同注入，因此平台 machine bearer token 没有透传通道。
- `Discover` 要求 `server/discover` 协商结果仍为 `2026-07-28`、stateless 且声明 Tools capability；`tools/list` 最多读取 20 页/1000 个 Tool，拒绝 cursor 循环和重复 Tool 名。
- Tool name/title/description/input/output Schema/annotations 被规范化并计算内容 SHA-256，写入 append-only discovery snapshot。未审核 snapshot 不会进入 Catalog 发布合同。
- `mcp_tool_routes` 把一个 Mender ToolVersion 固定到**精确 snapshot hash + upstream tool name + deployment revision**。每次 `tools/call` 前重新发现并核对 hash；drift 会在真正调用前 fail closed。
- `tools/call` 不自动网络重试。已知 success / tool-level error 会先写入 Workspace-RLS 的 `supply.mcp_call_results`，再向 Execution 返回 accepted；后续通过既有 Provider Status → Provider Observation → Artifact / Settlement 收敛。网络结果不明或 input-required 不会伪造成失败或成功，只返回 unknown 供现有 reconciliation 语义处理。

## 第三切片：reviewed runtime 与集成验证

- 新增独立 `mender_mcp_connector` 角色及 `grant-mcp-connector` 操作员命令；connector 只能读取 admitted input/current Connection/reviewed Deployment，并读写 Supply-owned MCP snapshot/route/call-result evidence，不能读取平台 API key、Commerce、Catalog、Distribution 或修改 Execution 状态。
- `BuildSupplierMCPRuntime` 要求 Worker DB 与 connector DB 是同一数据库但不同受限 principal，同时必须显式注入 SecretProvider 与 egress allowlist。默认 Worker entrypoint 不从环境变量自动拼出这个 runtime，因此远程 MCP egress 仍是 fail closed。
- 本地 loopback MCP integration fixture 覆盖 discovery → snapshot → reviewed route → StartRun → Worker Dispatch → upstream `tools/call` → Supply result evidence → Provider Reconciler → Artifact/settlement 的完整链路，并检查 machine token 不透传、RLS、Connection revoke、disabled Deployment 和只调用一次语义。
- 已通过 integration-tag 编译、`pnpm check`、`go test -race -count=1 ./...` 与 `git diff --check`。当前自动化环境的 Docker daemon 不可用，因此本轮尚缺 `pnpm test:integration:docker` 的真实 PostgreSQL 执行证据；在该门禁通过前不把第三切片或整个阶段标记为完成。

OAuth、Resources、Prompts、Tasks、MRTR、Agent/A2A 仍未实现；没有声明任何公网 Provider 已经生产接入。

## 2026-09-12 验证补充

上一阶段记录中的“Docker daemon 不可用”只描述当时宿主环境，不再代表当前门禁状态。2026-09-12 在当前 `main` 后续基线上重新执行 `pnpm test:integration:docker` 已通过真实隔离 PostgreSQL；Upstream MCP discovery/call/result convergence 与其余 migration/RLS/admission/provider-control 链路共同通过并清理自有容器。该结果仍只证明本地受控 fixture，不代表公网 MCP Provider、OAuth 或未实现能力已经生产验证。

