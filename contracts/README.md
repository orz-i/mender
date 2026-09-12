# Mender 接口合同

API、插件、工具和事件的设计合同，供接口评审与实现使用。业务接口尚在开发中。

| 文件 | 内容 |
| --- | --- |
| [public-api.openapi.yaml](public-api.openapi.yaml) | OpenAPI 3.1 消费 API，覆盖工具发现与任务执行 |
| [run-read.openapi.yaml](run-read.openapi.yaml) | 已实现的授权 Run 列表和有限事件时间线；独立开关、签名游标；已通过隔离 PostgreSQL 子集测试 |
| [artifact-read.openapi.yaml](artifact-read.openapi.yaml) | v0.2.0：已实现的机器凭据与 Human Run delegation 受保护 Artifact list/detail；只包含 bounded inline JSON result，不声明对象存储或签名 URL |
| [mcp-meta-tools.json](mcp-meta-tools.json) | MCP 2026-07-28 stateless 网关的四个已实现平台元工具、输入/输出和明确未承诺能力 |
| [fixed-toolset-contract.json](fixed-toolset-contract.json) | 已实现 Fixed Toolset MCP：不可变 Tool Schema、稳定别名、固定 Connection、`_mender` 幂等/费用控制和异步 Run receipt；不把业务 Artifact 冒充同步 Tool result |
| [upstream-mcp-contract.json](upstream-mcp-contract.json) | 远程 MCP Tools 接入的第一阶段合同：固定 reviewed endpoint、2026-07-28 stateless、禁止 machine token passthrough、append-only discovery snapshot；当前仅合同/持久化基础，不声明网络 Client 已实现 |
| [run-query-cancel.openapi.yaml](run-query-cancel.openapi.yaml) | v0.3.0：机器认证查询／取消，可选独立角色的协调取消；默认关闭，已通过本地真实数据库子集测试，不代表商业验收 |
| [plugin-manifest.schema.json](plugin-manifest.schema.json) | 声明式插件清单与制品摘要约束 |
| [tool.example.json](tool.example.json) · [http-executor.example.json](http-executor.example.json) | 工具定义与 HTTP 执行映射 |
| [connection-config.schema.json](connection-config.schema.json) · [connection-ui.example.json](connection-ui.example.json) | 授权连接配置与表单描述 |
| [event-envelope.schema.json](event-envelope.schema.json) · [event.example.json](event.example.json) | 平台业务事件结构与样例 |

在仓库根运行 `pnpm check:contracts`，校验 Schema、样例、制品摘要、OpenAPI 引用和操作定义。独立 Python 校验器 `validate_contracts.py` 需要 PyYAML 与 jsonschema。

示例使用 `example.test` 和凭据引用，不含真实密钥。当前可运行的 API 见[开发指南](../docs/engineering/development.md)。
