# MCP Meta-Tool Gateway Foundation

阶段状态：实现完成并通过本地协议、架构和真实 PostgreSQL 验证。目标是把 Mender 已有的 Admission、Run、Artifact 能力通过官方 MCP Go SDK 暴露为受认证的平台元工具，而不是提前宣称任意 Toolset 已能直接作为 MCP Tools 发布。

协议基线固定为官方 `github.com/modelcontextprotocol/go-sdk` v1.7.0；该稳定版本实现 MCP `2026-07-28`。Mender 首个 HTTP 入口只认证并测试 `2026-07-28`，使用 stateless Streamable HTTP。SDK 同时保留旧协议兼容代码，但本阶段不把旧版本列入 Mender 的兼容承诺。

首个入口为 `/mcp/v1/workspaces/{workspace_id}`。HTTP Bearer machine Key 在进入 SDK handler 前认证，并与路径 Workspace 交叉验证。MCP request/session 身份不作为 Mender 授权凭据。后续元工具继续走现有 `run:create`、`run:read`、`run:cancel` 和 Admission 预算／Connection／Toolset 规则。

DDD 边界：MCP SDK 仅允许出现在 `processes/mcpbridge/adapters/inbound`；mcpbridge application 使用消费方端口；跨 identity、admission、execution 均由 outbound ACL 依赖对方 public 合同。bootstrap 只负责显式装配。

本阶段不包括固定 Toolset direct tools、上游 MCP Client、OAuth、Resources／Prompts、MRTR、Agent/A2A、对象存储或支付。固定 Toolset direct tools 需要后续补齐 Catalog 的正式输入 Schema／描述合同和 Toolset Connection／费用策略快照后再实现。

## 已实现工具

| 工具 | 复用的现有能力 | 关键输入／输出 |
| --- | --- | --- |
| `mender_run_start` | Admission `run:create`、Toolset/Connection/Price/Budget 解析与原子受理 | 显式 idempotency key、tool/version、toolset、connection、arguments、currency、max charge；返回 Run/reservation 摘要 |
| `mender_run_get` | Execution `run:read` | Run ID → 状态、版本和时间；不返回凭据/Provider 控制字段 |
| `mender_run_cancel` | Execution `run:cancel`＋协调取消／Provider cancel intent 语义 | Run ID＋原因 → 当前取消状态；不声称远端已经停止 |
| `mender_artifact_get` | Artifact `run:read` | Run/Artifact ID → bounded inline JSON result；隐藏 source observation/provider handles |

`mender_run_start.arguments` 在低层 SDK `Server.AddTool` handler 中保持 `json.RawMessage`，严格拒绝未知顶层字段并把原始 JSON object bytes 交给 Admission。因此 `9007199254740993` 之类整数不会先经过 `float64` 再参与 canonical request/idempotency 逻辑。金额和 Run version 在 MCP 输出中使用十进制字符串。

## 验证边界

官方 SDK client 测试确认协商版本为 `2026-07-28`、stateless session ID 为空、工具列表严格为四个元工具，并覆盖缺失认证、跨 Workspace、旧协议头、未知字段和脱敏输出。隔离 PostgreSQL E2E 进一步验证 MCP StartRun 真实预留 quota、exact arguments、相同 key replay、同 key 改请求冲突、`run:read`／`run:create` scope、协调取消释放 held quota，以及读取既有 Provider Result Artifact。

MCP HTTP request cancellation 由 SDK `PropagateRequestCancellation` 传播到当前 handler。若 Admission 已经在断线前完成 durable commit，Run 不会因为 HTTP 响应丢失被自动取消；调用者应使用相同 idempotency key 重试 StartRun。该行为与 Mender 现有“外部副作用不宣称 exactly-once”的可靠性模型一致。
