# Mender 当前实施与验收状态

> 自动生成：`node scripts/render-project-status.mjs --write`。唯一可编辑状态源是 [project-data.json](project-data.json) 的任务 `execution` 字段；不要手改本页。

复核日期：2026-09-16。本地复核提交：`bfa8171409e5cc1baea3a89df6d23d94212ba4b9`。

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
| G4 | pending | 后端切片不代替完整商业准备验收 |
| G5 | pending | 未据此授予生产／商业 Beta 放行 |

**remote-merge-protection — not-verified：** 本轮不变更 GitHub 设置。仓库 CI 运行不等于主干强制门禁；需管理员核验并配置必需 check，记录真实证据。
**external-acceptance-and-live-payments — pending：** 第三方客户端、用户反馈、商户资格与真实支付未因内部测试或 sandbox 完成而取得认证。

## 原 S4 全量范围

| 原任务 | 实施 | 自动化证据 | 原任务验收 | 已有范围／剩余条件 |
| --- | --- | --- | --- | --- |
| S4-01 实现插件清单校验与发布状态机 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 受审清单、发布状态机、精确审批与后端 API；**剩余：**全量原任务验收；发布者／审核 UI 按 S4-09/10，非本切片已完成 |
| S4-02 实现危险操作审批与 JIT 管理授权 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 限定危险动作与 JIT；后续增加 refund/adjustment 消费者；**剩余：**全量原任务验收；S4-10 UI 与生产 MFA 不自动完成 |
| S4-03 实现账单、调账和退款补充分录 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 独立业务账本、追加分录、退款／调账及对账后端；**剩余：**原范围验收；ProviderCostEvent、真实资金及 S4-11 UI 仍有缺口 |
| S4-04 实现灰度、排空、回退和禁用开关 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | cohort 灰度、排空、回退、紧急禁用与固定 Run 版本；**剩余：**原任务验收；百分比灰度／自动指标晋级非已实现范围 |
| S4-05 实现供应商与平台 Admin 管理能力 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 工作区冻结、Provider 隔离、事故与审计后端；**剩余：**商业供应商准入、S4-10 平台运营 UI 及原任务验收 |
| S4-06 实现支付供应商适配与回调对账 | 限定范围已实现 | 已有记录（非本次重跑） | 待验收 | 供应商中立 sandbox PaymentIntent、验签与回调对账；**剩余：**原任务 sandbox 条件签署；真实 PSP／商户资格／真钱与 S4-11 UI 不得据此放行 |
| S4-07 实现 CLI 与 Skill 分发合同 | 未开始 | 未执行 | 待验收 | 当前切片均明确 CLI/Skill 分发后置；**剩余：**原 CLI/Skill 合同、安装与多入口一致性测试 |
| S4-08 实现发布质量与健康检查任务 | 待复核 | 待复核 | 待验收 | 未取得发布质量／健康任务完整交付证据；**剩余：**核对真实实现再决定工作量；不能把 API health 探针当合成质量任务 |
| S4-09 实现发布者草稿、测试与提交页面 | 未开始 | 未执行 | 待验收 | 发布者工作台在插件证据中仍 deferred；**剩余：**发布者草稿、测试、提交页面及 T31 浏览器验收 |
| S4-10 实现 Admin 审核、异常与 JIT 界面 | 部分实现 | 待复核 | 待验收 | 已有 Catalog 审核／策略页面；新的发布、异常、JIT UI 尚缺；**剩余：**接入现有 S4-02/05 后端；敏感操作、权限、失败路径和浏览器验收 |
| S4-11 实现账单、支付及退款状态界面 | 未开始 | 未执行 | 待验收 | 账务与支付证据明确 UI deferred；**剩余：**账单／支付／退款状态页面及非成功跳转真源验收 |
| S4-12 补齐文档、API 示例与发布引导 | 部分实现 | 待复核 | 待验收 | 已有合同及局部工程说明，本轮仅修正当前状态入口；**剩余：**CLI／发布者路径齐备后的完整文档、示例与用户失败指引 |
| S4-13 测试发布、回退、审批与 Admin | 部分实现 | 已有记录（非本次重跑） | 待验收 | 插件、发布、审批、Admin 后端已有测试文件和范围记录；**剩余：**S4-09/10 浏览器和端到端场景；原 T22–T24/T26/T31 全条件核对 |
| S4-14 测试支付、对账与多入口一致性 | 部分实现 | 已有记录（非本次重跑） | 待验收 | 业务账本和模拟支付已有自动化测试记录；**剩余：**CLI/Skill、多入口及 S4-11 UI 联调；原 T20/T21/T27/T29 全条件核对 |
| S4-15 执行商业化准备回归与 G4 证据整理 | 待复核 | 待复核 | 待验收 | 尚无完整 G4 放行证据；**剩余：**汇总 S4-13/14、明确剩余条件并形成正式 G4 决议 |
| S4-16 实现制品校验、签名与依赖扫描 | 部分实现 | 已有记录（非本次重跑） | 待验收 | 受审 manifest／权限验证存在，签名和依赖扫描未全量认证；**剩余：**制品签名、依赖扫描及准入阻断实测，不能用 checksum 代替 |
| S4-17 配置发布回退、备份与对账值班入口 | 部分实现 | 待复核 | 待验收 | 局部运行边界和回退 API 有说明；**剩余：**实际备份恢复、值班责任、外部告警与事故演练 |
| S4-18 确认服务条款、收费与供应商清单 | 待复核 | 待复核 | 待验收 | 未取得真实商户／地区／供应商许可与商业签署证据；**剩余：**由业务责任人确认服务条款、收费、数据和供给许可；不得自动批准 |

## 下一步仍使用原任务

- **S4-10 实现 Admin 审核、异常与 JIT 界面**：接入现有 S4-02/05 后端；敏感操作、权限、失败路径和浏览器验收
- **S4-09 实现发布者草稿、测试与提交页面**：发布者草稿、测试、提交页面及 T31 浏览器验收
- **S4-11 实现账单、支付及退款状态界面**：账单／支付／退款状态页面及非成功跳转真源验收
- **S4-13 测试发布、回退、审批与 Admin**：S4-09/10 浏览器和端到端场景；原 T22–T24/T26/T31 全条件核对
- **S4-14 测试支付、对账与多入口一致性**：CLI/Skill、多入口及 S4-11 UI 联调；原 T20/T21/T27/T29 全条件核对
- **S4-15 执行商业化准备回归与 G4 证据整理**：汇总 S4-13/14、明确剩余条件并形成正式 G4 决议
- **S4-16 实现制品校验、签名与依赖扫描**：制品签名、依赖扫描及准入阻断实测，不能用 checksum 代替
- **S4-17 配置发布回退、备份与对账值班入口**：实际备份恢复、值班责任、外部告警与事故演练
- **S4-18 确认服务条款、收费与供应商清单**：由业务责任人确认服务条款、收费、数据和供给许可；不得自动批准

当前纠偏只同步状态与证据检查，不将这些待交付页面、演练和商业审批标成完成。

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
| S4-01 实现插件清单校验与发布状态机 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json) |
| S4-02 实现危险操作审批与 JIT 管理授权 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4c-dangerous-operation-jit-evidence.json)、[证据2](../engineering/s403-commerce-billing-evidence.json) |
| S4-03 实现账单、调账和退款补充分录 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s403-commerce-billing-evidence.json) |
| S4-04 实现灰度、排空、回退和禁用开关 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4b-release-governance-evidence.json) |
| S4-05 实现供应商与平台 Admin 管理能力 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4d-platform-admin-evidence.json) |
| S4-06 实现支付供应商适配与回调对账 | 待验收 | 限定范围已实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s406-sandbox-payment-evidence.json) |
| S4-07 实现 CLI 与 Skill 分发合同 | 未开始 | 未开始／未执行／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json) |
| S4-08 实现发布质量与健康检查任务 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S4-09 实现发布者草稿、测试与提交页面 | 未开始 | 未开始／未执行／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../../frontend/apps/console/src/app/router.tsx) |
| S4-10 实现 Admin 审核、异常与 JIT 界面 | 进行中 | 部分实现／待复核／待验收 | [证据1](../../frontend/apps/admin/src/app/router.tsx)、[证据2](../engineering/s4a-plugin-publication-evidence.json)、[证据3](../engineering/s4b-release-governance-evidence.json)、[证据4](../engineering/s4c-dangerous-operation-jit-evidence.json)、[证据5](../engineering/s4d-platform-admin-evidence.json) |
| S4-11 实现账单、支付及退款状态界面 | 未开始 | 未开始／未执行／待验收 | [证据1](../engineering/s403-commerce-billing-evidence.json)、[证据2](../engineering/s406-sandbox-payment-evidence.json)、[证据3](../../frontend/apps/admin/src/app/router.tsx) |
| S4-12 补齐文档、API 示例与发布引导 | 进行中 | 部分实现／待复核／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s406-sandbox-payment-evidence.json) |
| S4-13 测试发布、回退、审批与 Admin | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json)、[证据3](../engineering/s4c-dangerous-operation-jit-evidence.json)、[证据4](../engineering/s4d-platform-admin-evidence.json)、[证据5](../../backend/tests/integration/postgres_test.go) |
| S4-14 测试支付、对账与多入口一致性 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s403-commerce-billing-evidence.json)、[证据2](../engineering/s406-sandbox-payment-evidence.json)、[证据3](../../backend/tests/integration/postgres_test.go) |
| S4-15 执行商业化准备回归与 G4 证据整理 | 待复核 | 待复核／待复核／待验收 | 未复核；不推断未实现 |
| S4-16 实现制品校验、签名与依赖扫描 | 进行中 | 部分实现／已有记录（非本次重跑）／待验收 | [证据1](../engineering/s4a-plugin-publication-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json) |
| S4-17 配置发布回退、备份与对账值班入口 | 进行中 | 部分实现／待复核／待验收 | [证据1](../engineering/alpha-operations-evidence.json)、[证据2](../engineering/s4b-release-governance-evidence.json) |
| S4-18 确认服务条款、收费与供应商清单 | 待复核 | 待复核／待复核／待验收 | [证据1](../engineering/s406-sandbox-payment-evidence.json) |
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
