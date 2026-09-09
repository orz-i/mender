# Mender 工程治理

[开发指南](development.md)介绍环境与日常命令，[工程基线](initialization-report.md)记录当前实现。

| 文件 | 用途 |
| --- | --- |
| [architecture-policy.yaml](architecture-policy.yaml) | GVR-01–GVR-16 依赖与工程约束，待完整接入自动检查 |
| [context-map.json](context-map.json) | 8 个候选上下文的职责、数据所有权与公开合同 |
| [上下文卡模板](templates/CONTEXT_CARD_TEMPLATE.md) | 领域建模和所有权评审 |
| [架构例外模板](templates/ARCHITECTURE_EXCEPTION_TEMPLATE.md) | 例外范围、负责人、到期日和退出条件 |
| [PR 模板](../../.github/pull_request_template.md) | 变更用例、合同、数据与验证记录 |

工具链以根 [toolchain.versions.json](../../toolchain.versions.json) 为准，依赖由 pnpm 与 Go 锁文件固定。治理规则中的版本约束待 G0 评审。

CI 已覆盖基础代码与构建检查；导入图、跨域数据访问、原子事务、秘密扫描和生成漂移检查按计划补齐。
