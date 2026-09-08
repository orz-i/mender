# Mender 工程治理资料

[开发指南](development.md) 与 [初始化报告](initialization-report.md) 描述当前仓库。以下文件来自设计 v1.1，保留其待评审或未实施状态：

| 文件 | 用途 |
| --- | --- |
| [architecture-policy.yaml](architecture-policy.yaml) | GVR-01–GVR-16 的设计政策；未连接完整架构扫描器 |
| [context-map.json](context-map.json) | 8 个候选上下文和职责、所有权、预期公开合同 |
| [上下文卡模板](templates/CONTEXT_CARD_TEMPLATE.md) | 后续建模和所有权评审 |
| [架构例外模板](templates/ARCHITECTURE_EXCEPTION_TEMPLATE.md) | 记录范围、负责人、到期日和退出条件 |
| [PR 模板](../../.github/pull_request_template.md) | 按实际变更填写用例、合同、数据和验证 |

原始政策中的工具链 null 表示历史设计尚未选版，不是当前安装配置。可执行工具链以根 [toolchain.versions.json](../../toolchain.versions.json)、[package.json](../../package.json)、[pnpm-workspace.yaml](../../pnpm-workspace.yaml) 和 Go 模块为准。当前验证范围与原政策保持显式区分，不将全部 GVR 标记为通过。

CI 已接线工具链／工作区检查、文档完整性、合同结构、lint、类型、基础测试及双入口构建。尚未接线完整导入图、符号级纯度、跨域写表、数据库权限、原子事务、秘密扫描和生成代码漂移检查。没有真实生成的业务 API 客户端需要比对，也没有初始化时编造的架构例外白名单。
