# Mender 文档索引

项目名称于本次初始化统一为 **Mender**。设计来源是 2026-09-04 的 MCPX 设计交付包 v1.1，归档日期为 2026-09-08。设计建议、历史检查与当前实现分开记录。

## 当前工作文档

| 文档 | 内容与状态 |
| --- | --- |
| [产品与系统架构设计](design/01_Mender_产品与系统架构设计.md) | A00–A18；Mender 更名副本，业务设计与候选边界待评审 |
| [分阶段实施与验收计划](design/02_Mender_分阶段实施与验收计划.md) | B00–B13；保留 20 周、六阶段原计划，不承诺实际开工日期 |
| [DDD 与工程治理规范](design/03_Mender_DDD与工程治理规范.md) | G00–G10；完整治理目标，实施情况以初始化报告为准 |
| [架构配图](diagrams/README.md) | 7 张 PNG 和同名 DOT；从原包逐字节复制 |
| [实施计划与台账](planning/README.md) | 92 项任务等结构化基线、原始 Excel 入口、后续优先事项 |
| [开发指南](engineering/development.md) | 环境、安装、启动、构建、配置和常见问题 |
| [初始化报告](engineering/initialization-report.md) | 当前范围、实际验证证据、未完成项与 WBS 映射 |
| [治理文件说明](engineering/README.md) | 原设计政策、上下文映射、模板与 CI 边界 |
| [更名记录](decisions/0001-project-name.md) | Mender 命名规则、历史文件和协议标识例外 |
| [初始化技术决策](decisions/0002-initialization-baseline.md) | 当前工具链和可逆工程选择；不替代 G0 决议 |
| [合同设计样例](../contracts/README.md) | JSON Schema、OpenAPI 设计子集、验证方式 |

## 原始交付归档

[归档说明](archive/design-delivery-v1.1/README.md) 包含来源 ZIP 的 SHA-256、文件数量和保真边界。[原始包索引](archive/design-delivery-v1.1/MCPX_Design_Package_v1.1/README.md) 包含 DOCX、PDF、XLSX、Markdown、合同与工程模板。

原始 DOCX／PDF／XLSX 中的 MCPX 标题保留用于追溯。当前可编辑设计正文位于 `design/`，计划快照位于 `planning/`；没有声称重新发布了一套 Mender Word／PDF／Excel 制品。需对外发布新版本时，从当前工作文档重新导出并检查版式。

## 维护规则

- 日常更新当前工作文档；不覆盖原始归档和原包校验报告。
- 修改设计、合同或计划时记录变更动机、关联任务、测试和 ADR；不自动同步 Office 制品。
- 实施证据记录执行命令、环境和结果。原包“通过”只表示历史文件检查，不能用作当前业务验收。
- 运行 `pnpm check:docs` 校验原始文件摘要、计划引用和当前 Markdown 本地链接；外部来源与 Office 版式需要另行核验。
