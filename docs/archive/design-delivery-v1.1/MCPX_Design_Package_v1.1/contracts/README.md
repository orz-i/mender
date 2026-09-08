# 合同设计样例

这些文件是架构基线的可校验样例，不是可部署服务，不包含真实密钥或已接通的供应商。

- `public-api.openapi.yaml`：10 条核心消费路径的 OpenAPI 3.1 设计子集；不包含全量 Console/Admin API，也不是 MCP JSON-RPC wire schema。
- `plugin-manifest.schema.json` 与 `plugin-manifest.example.json`：仅覆盖首发声明式 HTTP 插件；其他运行时需新增受审合同。
- `tool.example.json`、`http-executor.example.json` 与连接配置：示例执行映射与工具合同。`api.example.test` 不代表真实供应商。
- `event-envelope.schema.json` 与 `event.example.json`：平台业务事件；不是 MCP 协议通知。

`validate_contracts.py` 验证 JSON Schema、文件引用、制品摘要以及 OpenAPI 本地引用和路径参数。它不等于完整 OpenAPI 规范验证、MCP conformance、安全测试或业务集成测试。需要 Python 3、PyYAML 和 jsonschema。

运行：`python validate_contracts.py`。实现阶段应加入完整 OpenAPI linter、生成客户端的编译测试和真实端到端测试。
