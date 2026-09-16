# Mender 文档

## 当前实施

[当前实施与验收状态](planning/current-status.md)是统一状态入口，由[结构化主台账](planning/project-data.json)生成。限定实现、已有自动化记录、原任务验收与剩余条件分别展示；旧 Word／PDF／Excel 是 v1.1 历史设计快照，不是实时进度看板。状态字段和更新规则见[状态维护约定](planning/status-tracking.md)。

Core Alpha／G3 是内部范围化放行；S4 继续使用原 S4-01～S4-18，不把历史字母切片 COMPLETE 扩大为全阶段、外部客户端或生产验收。

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
| [协调取消与额度释放](engineering/2026-09-10-coordinated-cancellation.md) | 原周期释放、Run／Job／事件原子取消、独立角色与并发／回滚证据 |
| [内部原子受理](engineering/2026-09-09-atomic-admission.md) | Docker 真实验证、幂等受理、额度预留、Run／Job／Outbox 和取消保护 |
| [历史工程基线](engineering/initialization-report.md) | 早期实现与验证快照；当前状态见本页首部入口 |
| [execution 领域切片](engineering/2026-09-09-execution-foundation.md) | Run 聚合、查询／取消端口、架构门禁与覆盖边界 |
| [Run 列表与时间线](engineering/2026-09-09-run-read-models.md) | 授权查询、签名游标、有限事件水位与隔离 PostgreSQL 测试入口 |
| [身份与 PostgreSQL 切片](engineering/2026-09-09-identity-postgres.md) | 机器身份与持久化历史记录；当前验证见原子受理及协调取消记录 |
| [工程治理](engineering/README.md) | 依赖约束、上下文映射与协作模板 |
| [技术决策](decisions/0002-initialization-baseline.md) | 工具链与工程结构的选择依据 |
| [检查命令](../scripts/README.md) | 本地与 CI 检查入口 |

设计修订以 Markdown、合同为准；执行状态以主台账 `execution` 字段为准。Word、PDF、Excel 保留为明确标记的 v1.1 历史快照，本轮未重新导出。`pnpm check:docs` 检查引用；状态一致性检查不能替代真实测试、人工验收或远端门禁配置。
