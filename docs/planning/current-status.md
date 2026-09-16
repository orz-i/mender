# Mender 当前实施与验收状态

> 自动生成：`node scripts/render-project-status.mjs --write`。唯一可编辑状态源是 [project-data.json](project-data.json) 的任务 `execution` 字段；不要手改本页。

复核日期：2026-09-16。本地复核提交：`7549741603735fd2d2cb618864be5b38d77823a1`。

本页区分实施、自动化证据和原任务验收。已有证据文件不等于本次测试通过；切片 COMPLETE 不等于原任务完整验收。未复核的条目明确标记待复核，不推断为没有代码。

当前按原计划处于 **S4**。Core Alpha / G3 内部集成已范围化放行；第三方客户端、外部用户、生产运维与真实支付仍不据此获得认证。

## 计划与进度口径

保留 92 个原任务、原估算与 S0–S5 六阶段。历史执行字段保存在各任务 baseline_execution，Word／PDF／Excel 是 v1.1 历史快照，不作为当前进度看板。

没有完整验收证据的 progress 为 null，不能把旧的 0% 当作未开发，也不能按完成切片数计算总体完成率。手工批准、商业许可和远端门禁不由此文件自动授予。

## 历史别名与当前任务

| 历史切片名 | 原任务 |
| --- | --- |
| S4-A | S4-01 |
| S4-B | S4-04 |
| S4-C | S4-02 |
| S4-D | S4-05 |

旧证据文件按历史语义保留，不再创造新 S4 字母批次。后续工作从原 S4-01～S4-18 选择。

## Gate 与外部条件

| Gate | 当前记录 | 证据／限制 |
| --- | --- | --- |
| G3 | scoped-go | 限定内部集成；第三方客户端与外部用户验收继续待完成；[决议记录](../engineering/alpha-closure.json) |
| G4 | pending | S4本地工程与自动化验证已汇总；原任务剩余范围、正式用户验收、值班/远端门禁和商业签署仍待负责人确认，不自动授予G4商业放行 |
| G5 | pending | 未据此授予生产／商业 Beta 放行 |

**remote-merge-protection — not-verified：** 本轮不变更 GitHub 设置。仓库 CI 运行不等于主干强制门禁；需管理员核验并配置必需 check，记录真实证据。
**external-acceptance-and-live-payments — pending：** 第三方客户端、用户反馈、商户资格与真实支付未因内部测试或 sandbox 完成而取得认证。

## 原 S4 全量范围

| 原任务 | 实施 | 自动化证据 | 原任务验收 | 已有范围／剩余条件 |
| --- | --- | --- | --- | --- |
| S4-01 实现插件清单校验与发布状态机 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 受审manifest、发布状态机、精确独立审批和发布API保留；对应S4-09/10工作台已接入；**剩余：**原任务完整范围正式验收；生产供应商/插件准入条件不得由本地测试代签 |
| S4-02 实现危险操作审批与 JIT 管理授权 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 危险操作与JIT以及refund/adjustment消费者保留，新增精确审批读取和工作台；撤销权限错误保持403；**剩余：**原范围正式验收；生产MFA和泛化危险动作不由本轮认证 |
| S4-03 实现账单、调账和退款补充分录 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 不可变业务账本、追加分录、退款/调账及对账保留，S4-11界面与真实退款重放验证已接入；**剩余：**原范围验收；ProviderCostEvent及真实资金不在已认证范围 |
| S4-04 实现灰度、排空、回退和禁用开关 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | cohort灰度、排空、回退与紧急停用已接入工作台并纳入真实数据库回归；**剩余：**原任务验收；百分比灰度与自动指标晋级非当前范围 |
| S4-05 实现供应商与平台 Admin 管理能力 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 工作区冻结、Provider隔离、事故与审计后端及运营UI已接入，权限/版本冲突通过真实浏览器验证；**剩余：**商业供应商准入与正式运营验收；外部告警责任人须配置 |
| S4-06 实现支付供应商适配与回调对账 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 供应商中立sandbox意图、验签、回调与对账及状态UI保留，不提供真实充值；**剩余：**原sandbox范围签署；真实PSP/商户资格/地区/真钱仍不可放行 |
| S4-07 实现 CLI 与 Skill 分发合同 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 官方Go SDK CLI与Skill已实现；通过同MCP授权/预算/Run执行、幂等重放、查询、取消和精确参数合同；**剩余：**用户在已授权实际环境安装与接入验收；不声明未经验证的第三方客户端 |
| S4-08 实现发布质量与健康检查任务 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 只读MCP发现/合同快照、漂移拒绝、有界采样任务及无脚本质量报告已实现；不覆盖当前发布版本；**剩余：**负责人确认受控工具集探测范围；平台内多租户质量趋势持久化及付费上游功能任务不由此自动认证 |
| S4-09 实现发布者草稿、测试与提交页面 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 发布者工作台及真实Chromium/HTTP/隔离PostgreSQL草稿、预检、独立审核、发布与负向流程；**剩余：**首发发布者用户验收；上游功能试调用需真实授权且受限计费 |
| S4-10 实现 Admin 审核、异常与 JIT 界面 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 发布审核/回退、冻结/事故/审计和JIT工作台复用真实后端；CSRF、精确版本、不同审核主体及越权场景已验证；**剩余：**正式运营用户验收和生产身份/MFA保证；不得由浏览器确认替代服务端批准 |
| S4-11 实现账单、支付及退款状态界面 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 账本、对账、部分退款/调账审批与sandbox支付状态界面已有真实浏览器验证，不将跳转当到账；**剩余：**财务用户验收；真实商户/支付网络及商业资格仍阻断 |
| S4-12 补齐文档、API 示例与发布引导 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 已有实际CLI/Skill、五个工作台、质量、制品签名、依赖扫描和恢复引导；文档无真实秘密；**剩余：**面向首发用户复核文档和所有失败指引，正式产品验收待签署 |
| S4-13 测试发布、回退、审批与 Admin | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 发布/回退/审批/Admin合同、真实数据库及Chromium工作台回归已执行；不只核对文件存在；**剩余：**原T22～24/T26/T31的正式范围复核与真实用户验收 |
| S4-14 测试支付、对账与多入口一致性 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 不可变账本、sandbox回调、部分退款重放、浏览器账务与CLI同授权/同预算路径已执行；**剩余：**原T20/T21/T27/T29正式验收；实际PSP/商业环境不由测试替代 |
| S4-15 执行商业化准备回归与 G4 证据整理 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 汇总原18任务、实际命令/日志/源码指纹及G4阻断清单；工程验证不伪造Gate批准；**剩余：**项目负责人审阅范围并作出G4正式决议；现仍pending |
| S4-16 实现制品校验、签名与依赖扫描 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 真实Ed25519签名/外部可信公钥验证、不可变制品摘要与路径限制、网络依赖扫描已实现；修复两项实际可达漏洞；**剩余：**生产密钥托管/信任锚与部署准入联调；不可达依赖告警风险评审；远端CI与分支保护实测 |
| S4-17 配置发布回退、备份与对账值班入口 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 已提供工作台回退/对账手册与自有临时容器pg_dump/pg_restore，核对Run/Job/预算/账本/支付/迁移事实，恢复后不启动Worker；**剩余：**真实值班人、外部告警、备份保留与角色/密钥/对象存储恢复配置；生产RPO/RTO演练不在本地结果内 |
| S4-18 确认服务条款、收费与供应商清单 | 部分实现 | 已有记录（非本次重跑） | 待验收 | 商业前置条件/供应商权利/地区/数据/支付及责任人清单已整理，未取得外部证明或签署；**剩余：**由业务负责人提交真实服务条款、供应商分发许可、支付/数据地区依据并签署；不得自动批准 |

## 下一步仍使用原任务

- **S4-15 执行商业化准备回归与 G4 证据整理**：项目负责人审阅范围并作出G4正式决议；现仍pending
- **S4-17 配置发布回退、备份与对账值班入口**：真实值班人、外部告警、备份保留与角色/密钥/对象存储恢复配置；生产RPO/RTO演练不在本地结果内
- **S4-18 确认服务条款、收费与供应商清单**：由业务负责人提交真实服务条款、供应商分发许可、支付/数据地区依据并签署；不得自动批准

当前状态以逐项范围和真实执行回执为准；本地工程与自动化验证不能代替用户、商业及生产签署。

## 全任务实施状态与证据索引

| 任务 | 当前状态 | 实施／验证／验收 | 证据 |
| --- | --- | --- | --- |
| S0-01 冻结目标客户、首发边界与商业模式 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S0-02 开展领域建模并冻结上下文候选边界 | 进行中 | 部分实现／待复核／待验收 | [证据1](../engineering/context-map.json)、[证据2](../engineering/architecture-policy.yaml) |
| S0-03 验证 MCP 新旧协议与 SDK | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S0-04 验证 HTTP 与远程 Agent 最小调用 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S0-05 验证预留、重复消息及崩溃恢复 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-09-atomic-admission.md)、[证据2](../engineering/2026-09-10-coordinated-cancellation.md) |
| S0-06 设计控制台 IA 与关键页面低保真 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S0-07 定义验收矩阵与测试数据集 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S0-08 威胁模型与出口／凭据方案评审 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S0-09 申请供应商授权与确认试点名单 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S0-10 举行 G0 评审并冻结合同边界 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-01 建立按上下文分层的 Go 骨架与显式装配 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../../backend/internal/bootstrap/admission.go)、[证据2](../../backend/tests/architecture/README.md) |
| S1-02 创建租户、成员与服务身份模型 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-03 实现机器 Key 及权限策略接口 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-09-identity-postgres.md) |
| S1-04 实现登录会话与 OIDC 接口 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-12-human-session-oidc-foundation.md) |
| S1-05 实现 Catalog 与 Provider 读取模型 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-06 实现最小平台管理 API 与审计 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-07 搭建 pnpm workspace、Vite 与前端领域模块 | 进行中 | 部分实现／待复核／待验收 | [证据1](../../frontend/apps/console/src/app/router.tsx)、[证据2](../../frontend/apps/admin/src/app/router.tsx)、[证据3](../../pnpm-workspace.yaml) |
| S1-08 实现登录、Workspace 与成员界面 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-12-workspace-console-alpha.md)、[证据2](../../frontend/apps/console/src/app/router.tsx) |
| S1-09 实现目录、工具详情与 API Key 页面 | 进行中 | 部分实现／待复核／待验收 | [证据1](../../frontend/apps/console/src/app/router.tsx) |
| S1-10 实现最小 Admin 布局与工作区列表 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-11 搭建集成测试数据库与 fixture | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-12 验证身份、目录隔离与前端状态 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-13 建立 pnpm 冻结构建与架构门禁 CI | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../../scripts/check-architecture.mjs)、[证据2](../../scripts/architecture.test.mjs) |
| S1-14 配置开发／预发布环境及最小监控 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S1-15 完成 G1 演示与范围复核 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-01 实现 ToolVersion、价格版本与绑定 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-02 实现 Run、Attempt、事件与幂等 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-10-public-start-run.md)、[证据2](../engineering/2026-09-10-worker-leases.md) |
| S2-03 实现钱包、分录与原子预算预留 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-09-atomic-admission.md)、[证据2](../engineering/s403-commerce-billing-evidence.json) |
| S2-04 实现结算、释放与结果不明处理 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-11-commerce-usage-settlement.md)、[证据2](../engineering/s403-commerce-billing-evidence.json) |
| S2-05 实现 OpenAPI 导入器与诊断 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-06 实现 HTTP Executor 与受控转换 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-10-http-executor.md)、[证据2](../engineering/g3-evidence.json) |
| S2-07 实现 SQL 任务租约与 Outbox | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-10-worker-leases.md)、[证据2](../engineering/2026-09-10-dispatch-supervisor.md) |
| S2-08 实现执行、报价与结果 REST API | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-10-public-start-run.md)、[证据2](../engineering/2026-09-12-run-quota-cost-alpha.md) |
| S2-09 实现导入、测试与工具版本界面 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-10 实现 Run Explorer 与详情时间线 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-12-console-run-results-alpha.md)、[证据2](../../frontend/apps/console/src/app/router.tsx) |
| S2-11 实现预算、余额与费用明细 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-12-console-usage-observability-alpha.md) |
| S2-12 实现可验证的 Get Started 流程 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-13 编写 HTTP 导入与执行合同测试 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-14 实施幂等、预算及 Worker 故障注入 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-10-coordinated-cancellation.md)、[证据2](../engineering/g3-evidence.json) |
| S2-15 执行端到端 Alpha 验收 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/alpha-closure.json)、[证据2](../engineering/g3-evidence.json) |
| S2-16 部署出口限制与 Credential Broker 基础 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-17 建立 Run／资金监控及告警 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S2-18 组织试点任务与 G2 Alpha 评审 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S3-01 实现 Toolset 与路由快照发布 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-11-fixed-toolset-mcp.md)、[证据2](../engineering/s4b-release-governance-evidence.json) |
| S3-02 实现 OAuth Connection 与授权范围 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S3-03 实现产物服务与对象级授权 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json)、[证据2](../engineering/alpha-operations-evidence.json) |
| S3-04 实现受信回调、Inbox 与状态归并 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json)、[证据2](../engineering/alpha-operations-evidence.json) |
| S3-05 实现远程 MCP Client 适配 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S3-06 实现固定工具与动态元工具分发 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json)、[证据2](../engineering/alpha-closure.json) |
| S3-07 实现远程 Agent Adapter | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S3-08 实现 MCP 取消、异步启动与兼容测试钩子 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json)、[证据2](../engineering/alpha-operations-evidence.json) |
| S3-09 实现 Toolset 配置与安装体验 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S3-10 实现 Connection 与授权状态页面 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-12-connection-oauth-alpha.md)、[证据2](../../frontend/apps/console/src/app/router.tsx) |
| S3-11 实现 Agent 运行与产物界面 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../../frontend/apps/console/src/app/router.tsx)、[证据2](../engineering/g3-evidence.json) |
| S3-12 补齐多入口引导及兼容状态 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S3-13 执行协议兼容与头部授权测试 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S3-14 执行 OAuth、Agent、回调与产物测试 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json) |
| S3-15 执行三类能力端到端内测 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/g3-evidence.json)、[证据2](../../backend/tests/integration/g3_entry_matrix_test.go) |
| S3-16 配置流式代理、超时与网络策略 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/alpha-operations-evidence.json) |
| S3-17 配置对象保留和授权回调安全 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/alpha-operations-evidence.json) |
| S3-18 完成客户端验收名单与 G3 评审 | 进行中 | 部分实现／已有记录（非本次重跑）／范围化放行 | [证据1](../engineering/alpha-closure.json)、[证据2](../engineering/g3-evidence.json) |
| S4-01 实现插件清单校验与发布状态机 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/2026-09-16-s4-workbenches.md)、[证据3](../engineering/2026-09-16-s4-closeout.md) |
| S4-02 实现危险操作审批与 JIT 管理授权 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4c-dangerous-operation-jit-evidence.json)、[证据2](../engineering/s403-commerce-billing-evidence.json)、[证据3](../engineering/2026-09-16-s4-workbenches.md)、[证据4](../engineering/2026-09-16-s4-closeout.md) |
| S4-03 实现账单、调账和退款补充分录 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s403-commerce-billing-evidence.json)、[证据2](../engineering/2026-09-16-s4-workbenches.md)、[证据3](../engineering/2026-09-16-s4-closeout.md) |
| S4-04 实现灰度、排空、回退和禁用开关 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4b-release-governance-evidence.json)、[证据2](../engineering/2026-09-16-s4-workbenches.md)、[证据3](../engineering/2026-09-16-s4-closeout.md) |
| S4-05 实现供应商与平台 Admin 管理能力 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4d-platform-admin-evidence.json)、[证据2](../engineering/2026-09-16-s4-workbenches.md)、[证据3](../engineering/2026-09-16-s4-closeout.md) |
| S4-06 实现支付供应商适配与回调对账 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s406-sandbox-payment-evidence.json)、[证据2](../engineering/2026-09-16-s4-workbenches.md)、[证据3](../engineering/2026-09-16-s4-closeout.md) |
| S4-07 实现 CLI 与 Skill 分发合同 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json)、[证据3](../engineering/2026-09-16-s4-distribution-quality.md)、[证据4](../engineering/2026-09-16-s4-closeout.md) |
| S4-08 实现发布质量与健康检查任务 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-16-s4-distribution-quality.md)、[证据2](../engineering/2026-09-16-s4-closeout.md) |
| S4-09 实现发布者草稿、测试与提交页面 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../../frontend/apps/console/src/app/router.tsx)、[证据3](../engineering/2026-09-16-s4-workbenches.md)、[证据4](../engineering/2026-09-16-s4-closeout.md) |
| S4-10 实现 Admin 审核、异常与 JIT 界面 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../../frontend/apps/admin/src/app/router.tsx)、[证据2](../engineering/s4a-plugin-publication-evidence.json)、[证据3](../engineering/s4b-release-governance-evidence.json)、[证据4](../engineering/s4c-dangerous-operation-jit-evidence.json)、[证据5](../engineering/s4d-platform-admin-evidence.json)、[证据6](../engineering/2026-09-16-s4-workbenches.md)、[证据7](../engineering/2026-09-16-s4-closeout.md) |
| S4-11 实现账单、支付及退款状态界面 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s403-commerce-billing-evidence.json)、[证据2](../engineering/s406-sandbox-payment-evidence.json)、[证据3](../../frontend/apps/admin/src/app/router.tsx)、[证据4](../engineering/2026-09-16-s4-workbenches.md)、[证据5](../engineering/2026-09-16-s4-closeout.md) |
| S4-12 补齐文档、API 示例与发布引导 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s406-sandbox-payment-evidence.json)、[证据3](../engineering/2026-09-16-s4-closeout.md) |
| S4-13 测试发布、回退、审批与 Admin | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json)、[证据3](../engineering/s4c-dangerous-operation-jit-evidence.json)、[证据4](../engineering/s4d-platform-admin-evidence.json)、[证据5](../../backend/tests/integration/postgres_test.go)、[证据6](../engineering/2026-09-16-s4-workbenches.md)、[证据7](../engineering/2026-09-16-s4-closeout.md) |
| S4-14 测试支付、对账与多入口一致性 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s403-commerce-billing-evidence.json)、[证据2](../engineering/s406-sandbox-payment-evidence.json)、[证据3](../../backend/tests/integration/postgres_test.go)、[证据4](../engineering/2026-09-16-s4-closeout.md) |
| S4-15 执行商业化准备回归与 G4 证据整理 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/2026-09-16-s4-closeout.md) |
| S4-16 实现制品校验、签名与依赖扫描 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json)、[证据3](../engineering/2026-09-16-s4-distribution-quality.md)、[证据4](../engineering/2026-09-16-s4-closeout.md) |
| S4-17 配置发布回退、备份与对账值班入口 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/alpha-operations-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json)、[证据3](../engineering/2026-09-16-s4-operations-runbook.md)、[证据4](../engineering/2026-09-16-s4-closeout.md) |
| S4-18 确认服务条款、收费与供应商清单 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s406-sandbox-payment-evidence.json)、[证据2](../engineering/2026-09-16-s4-closeout.md) |
| S5-01 完成后端一致性与迁移加固 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-02 参与恢复、账本与事故演练修复 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-03 完成协议、超时及连接故障加固 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-04 完成容量、重试与背压优化 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-05 完成前端质量、可访问性与反馈修复 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-06 完成试点埋点与发布说明 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-07 执行隔离、SSRF、秘密及权限安全回归 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-08 执行稳态、突发与长任务性能测试 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-09 执行回退、恢复与未知任务演练 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-10 执行最终回归与 Go／No-Go 核验 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-11 执行灾备、密钥轮换和供应链演练 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-12 执行金丝雀部署与值班交接 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S5-13 组织试点、验收签署及范围归档 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |

## 检查边界

`pnpm check:status` 检查原任务编号、别名、三态关系、证据摘要、收口限制和本页生成漂移；它不会运行数据库／浏览器测试，也不能证明人工批准或 GitHub 分支保护已生效。业务测试仍由原 pnpm / Go / PostgreSQL 检查入口实际运行。
