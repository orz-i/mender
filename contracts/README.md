# Mender 合同设计样例

来源：设计交付包 v1.1。展示标题已统一为 Mender，业务字段、插件版本、媒体类型、示例 Schema URL 和制品摘要保持原设计语义。历史协议标识的保留原因见 [更名记录](../docs/decisions/0001-project-name.md)。

- `public-api.openapi.yaml`：OpenAPI 3.1 消费 API 设计子集，不含完整 Console／Admin API，也不是 MCP wire schema。
- `plugin-manifest.*`：声明式 HTTP 插件设计合同，含本地制品 SHA-256。
- `tool.example.json`、`http-executor.example.json`、`connection-*`：工具映射和引用凭据配置。
- `event-envelope.schema.json`、`event.example.json`：平台业务事件设计，不是 MCP 协议通知。

在仓库根执行：

```sh
pnpm check:contracts
```

Node 校验使用锁定的 Ajv 2020／ajv-formats／YAML，复核 Schema、代表性样例、制品摘要、受限本地引用、OpenAPI 操作编号与路径参数。保留的 `validate_contracts.py` 是历史校验器的更名副本，若单独使用需要 Python、PyYAML 和 jsonschema；它不是当前 CI 的运行依赖。

这些合同对应的业务接口尚未实现。当前 API 只有 `/healthz` 与 `/readyz`，记录于 [开发指南](../docs/engineering/development.md)。本目录校验不替代完整 OpenAPI 校验、协议一致性、安全测试、并发资金验证或真实供应商集成。

示例不包含真实凭据，`example.test` 不是实际供应商。将来生成业务 API 客户端时应记录生成器精确版本和再生成命令，再接入生成漂移检查。
