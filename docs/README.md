# Mender 文档

## 产品与设计

设计基线 v1.1，状态：待评审。

| 文档 | 在线阅读 | 下载 |
| --- | --- | --- |
| 产品与系统架构 | [Markdown](design/01_Mender_产品与系统架构设计.md) | [Word](design/01_Mender_产品与系统架构设计.docx) · [PDF](design/01_Mender_产品与系统架构设计.pdf) |
| 分阶段实施与验收 | [Markdown](design/02_Mender_分阶段实施与验收计划.md) | [Word](design/02_Mender_分阶段实施与验收计划.docx) · [PDF](design/02_Mender_分阶段实施与验收计划.pdf) |
| DDD 与工程治理 | [Markdown](design/03_Mender_DDD与工程治理规范.md) | [Word](design/03_Mender_DDD与工程治理规范.docx) · [PDF](design/03_Mender_DDD与工程治理规范.pdf) |

[架构配图](diagrams/README.md) · [实施计划与台账](planning/README.md) · [接口合同](../contracts/README.md)

## 开发与协作

| 文档 | 内容 |
| --- | --- |
| [开发指南](engineering/development.md) | 环境配置、本地启动、构建与常见问题 |
| [工程基线](engineering/initialization-report.md) | 当前实现、验证记录与待实现范围 |
| [工程治理](engineering/README.md) | 依赖约束、上下文映射与协作模板 |
| [技术决策](decisions/0002-initialization-baseline.md) | 工具链与工程结构的选择依据 |
| [检查命令](../scripts/README.md) | 本地与 CI 检查入口 |

设计修订以 Markdown、合同和结构化台账为准；Word、PDF、Excel 是 v1.1 文档快照，修订后需同步更新。`pnpm check:docs` 检查计划引用和本地文档链接。
