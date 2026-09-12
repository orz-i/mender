# Mender 工程基线

## 当前实现

### 2026-09-12 当前基线

Mender 已进入“可靠执行内核 + MCP 集成基础已验证，向可运行纵向 Alpha 收口”的阶段。当前实际实现已经超过本文件下方 2026-09-10 的早期描述：公共 StartRun、Worker lease/dispatch 基础、HTTP Provider submit/status/cancel、Provider reconciliation、fixed-success-only Usage Settlement、Provider Result Artifact、MCP Meta-Tool、Fixed Toolset MCP 以及 Upstream MCP Client 都已有实现和真实隔离 PostgreSQL 验证。最新切片见 [Runnable Vertical Alpha Foundation](2026-09-12-runnable-alpha-foundation.md)。

所有 StartRun 入口现在共享 ToolVersion 业务参数 Schema 校验：Schema 在 Admission 的 outbound capability adapter 中解释，REST／MCP 不再各维护一套规则，非法参数在 Connection/Pricing 和 quota reservation 前拒绝。受审 Worker runtime host 也已建立有界周期，可显式组合 dispatch、Provider Control/Reconciliation 与 Usage Settlement；默认 `cmd/worker` 仍不提供生产 SecretProvider 或 egress runtime，因此继续 fail closed。

本轮重新通过 `pnpm check`、真实隔离 PostgreSQL、Go race 与 `git diff --check` 的最终证据以 2026-09-12 阶段记录和会话 checkpoint 为准。下方标注 2026-09-10 的段落保留作为历史演进，不再作为“当前缺失能力”清单。

2026-09-10 更新：已新增带预留任务的协调取消与原周期额度释放。独立开关、取消角色、真实验证与当前边界以[阶段五收尾记录](2026-09-10-coordinated-cancellation.md)为准；下方早期记录不代表最新能力仍缺失。

Docker／专用测试环境阻塞已解除：用户完成隔离 PostgreSQL 测试，本轮也在当前工作树复核通过。新增内部原子受理、commerce 额度预留，以及同事务 Run／blocked Job／Outbox，见 [原子受理记录](2026-09-09-atomic-admission.md)。没有公共创建、实际收费或 Worker 执行入口；Run 列表仍需独立开关和安全注入的签名密钥。

- Go API 与 Worker，显式 bootstrap 和 8 个候选领域上下文。
- Console、Admin 两个 React 入口，包含首页、服务状态查询、失败重试和未知路由恢复。
- pnpm workspace，共享 UI、API client 和 TypeScript 配置；前后端使用固定工具链与依赖锁文件。
- GitHub Actions 基础 CI，以及工具链、工作区、文档、合同、代码和构建检查。
- Go／TypeScript 源码依赖边界检查接入 `pnpm check`，有正向和故意违规测试；尚不涵盖完整语义纯度或数据库所有权。
- execution 的 Run 聚合、授权前置的查询／取消用例、消费方端口与仅测试编译的内存仓储。
- identity 机器 Key／Workspace scope、PostgreSQL 出站仓储、迁移／操作员命令，以及显式开关启用的受保护 Run 查询／取消适配。
- v1.1 产品设计、实施计划、工程规范、架构图与任务台账。

默认 API 仍为 probes-only：`/healthz` 为 `200`、`/readyz` 为 `503`、业务路由为 `404`。开启 Run 子集需真实数据库、匹配迁移和受限角色；成功后的 readiness 仅覆盖已启用的查询能力，不代表商业平台就绪。真实数据库验证只在套件自有的临时资源上运行，未启用公网服务。Worker 仍未消费任务；未开启协调取消时，旧路径继续拒绝单独修改带预留任务；开启后仅对有完整未执行证据的任务原子取消与释放。

## 验证记录

2026-09-08，Windows / PowerShell，Node `24.19.0`、pnpm `11.18.0`、Go `1.26.7`：

| 检查 | 结果 |
| --- | --- |
| 冻结依赖安装与 `pnpm check` | 通过：工具链、工作区、文档、合同、lint、类型、测试与构建 |
| Go 测试、vet 与构建 | 通过：探针、业务路由边界、进程生命周期 |
| API client 测试 | 4 项通过：成功、HTTP 失败、非法响应、取消 |
| 本地 API 请求 | 存活 `200`、未就绪 `503`、业务路径 `404` |
| Console / Admin 浏览器检查 | 页面导航、真实状态查询、重试、键盘操作与窄屏布局通过 |

## 待实现／待产品化

公开注册／邀请和成员编辑、完整 Catalog／Toolset 管理、BYOK secret ingestion、OAuth refresh/rotation 与多 Provider 动态配置、云厂商/KMS SecretProvider、Outbox 对外投递、Webhook、Agent/A2A、对象存储、真实支付／复式账本／退款和完整 governance 业务能力仍按[实施计划](../planning/README.md)推进。人类 OIDC server-side session、Workspace 只读选择、Connection 安全元数据/撤销、单一 reviewed OAuth Connection 创建、服务端 launch discovery、Human→Run read/cancel delegation，以及精确短时 Human StartRun delegation 已落地。StartRun delegation 绑定一次 Idempotency-Key 和费用上限并复用原子 Admission；它仍不授予 billing administration 或 Toolset mutation。默认 Worker 仍不自动开启 Supplier runtime；只有显式 reviewed host 配置才会装配 mounted SecretProvider／Provider Control／Settlement。已有子集通过真实 PostgreSQL 不等于商业平台或 G0–G5 已验收。

| 任务 | 已有基础 | 后续验收重点 |
| --- | --- | --- |
| S0-02 / S0-10 | 上下文映射、工具链清单 | 统一语言、负责人、G0 决议与 ADR-020 |
| S1-01 | 后端分层、API / Worker 装配 | 领域用例、端口与依赖边界 |
| S1-07 | pnpm 单锁、双入口与共享 UI | 用户任务、前端依赖边界 |
| S1-13 | 基础检查与 CI | 导入图负向样例、生成漂移和秘密扫描 |
| S1-14 | 本地配置与探针 | 数据库、对象存储、预发布环境与监控 |

上述任务仍需完成对应验收条件；WBS 保持「未开始」，G0–G5 尚未验收。
