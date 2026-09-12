# Mender 工程治理

[开发指南](development.md)介绍环境与日常命令，[工程基线](initialization-report.md)记录当前实现。

| 文件 | 用途 |
| --- | --- |
| [architecture-policy.yaml](architecture-policy.yaml) | GVR-01–GVR-16 依赖与工程约束，待完整接入自动检查 |
| [context-map.json](context-map.json) | 8 个候选上下文的职责、数据所有权与公开合同 |
| [上下文卡模板](templates/CONTEXT_CARD_TEMPLATE.md) | 领域建模和所有权评审 |
| [架构例外模板](templates/ARCHITECTURE_EXCEPTION_TEMPLATE.md) | 例外范围、负责人、到期日和退出条件 |
| [PR 模板](../../.github/pull_request_template.md) | 变更用例、合同、数据与验证记录 |
| [2026-09-12 Runnable Vertical Alpha](2026-09-12-runnable-alpha-foundation.md) | 共享 StartRun Schema 校验、受审 Worker runtime host 与真实数据库证据 |
| [2026-09-12 Runnable Alpha 第二批](2026-09-12-runnable-alpha-second-slice.md) | mounted SecretProvider、显式 reviewed worker host 配置、Provider Control 与 Usage Settlement 证据 |
| [2026-09-12 Console Run Explorer](2026-09-12-console-run-explorer.md) | 首个真实 Console Execution 工作流、内存机器凭据边界与受保护 Run API 映射 |

工具链以根 [toolchain.versions.json](../../toolchain.versions.json) 为准，依赖由 pnpm 与 Go 锁文件固定。治理规则中的版本约束待 G0 评审。

CI／`pnpm check` 已覆盖前端解析后的导入图、Go AST 边界、负向 fixture、合同、lint、类型、测试与构建；真实 PostgreSQL 套件独立验证 RLS、角色、原子事务和执行链路。该治理仍不等于完整语义纯度、所有动态 SQL 的数据所有权证明、生产秘密扫描或生成物漂移证明，后续继续按计划补齐。
