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
| [2026-09-12 Human session / OIDC foundation](2026-09-12-human-session-oidc-foundation.md) | OIDC+PKCE、HttpOnly server-side session、CSRF、Workspace membership 与最小数据库角色 |
| [2026-09-12 Workspace Console Alpha](2026-09-12-workspace-console-alpha.md) | Operator 预配人类身份、Workspace 自助读取/选择与 same-origin Console session client |
| [2026-09-12 Connection self-service Alpha](2026-09-12-connection-self-service-alpha.md) | 独立最小 DB 角色、安全元数据列表、Workspace 授权与 CSRF 保护撤销 |
| [2026-09-12 Human → Run delegation Alpha](2026-09-12-human-run-delegation-alpha.md) | 短时显式 Run delegation、CSRF mint/revoke、当前 Membership 重校验与 Machine Key 隔离 |
| [2026-09-12 Reviewed OAuth Connection Alpha](2026-09-12-connection-oauth-alpha.md) | reviewed Provider OAuth + PKCE、服务端 Secret Vault、Connection/Grant 创建与 Console 发起授权 |
| [2026-09-12 Human launch discovery Alpha](2026-09-12-human-launch-discovery-alpha.md) | 服务端按 Membership/RLS/发布状态过滤 Toolset + Tool + Connection 启动投影，不授予 run:create |
| [2026-09-12 Human StartRun delegation Alpha](2026-09-12-human-start-run-delegation-alpha.md) | 精确绑定 Toolset/ToolVersion/Connection/费用上限/Idempotency-Key 的短时 run:create capability，复用原子 Admission |
| [2026-09-12 Console Human StartRun Alpha](2026-09-12-console-human-start-run-alpha.md) | `/launch` 从服务端能力发现到短时委托、同幂等重试、原子 Run 受理的首个 Human 产品闭环 |
| [2026-09-12 Console Artifact content Alpha](2026-09-12-console-artifact-content-alpha.md) | Machine Key + Human Run delegation 的 bounded inline JSON Artifact detail，补充 size/UTF-8 完整性检查 |
| [2026-09-12 Run Event pagination Alpha](2026-09-12-run-event-pagination-alpha.md) | Run Event cursor/through-version 有限快照分页，并正式覆盖 Human Run delegation Console 路径 |
| [2026-09-12 Console Run Results Alpha](2026-09-12-console-run-results-alpha.md) | Run Explorer 的 Event 分页、按需 Artifact JSON 内容预览与短期 delegation cache 清理 |

工具链以根 [toolchain.versions.json](../../toolchain.versions.json) 为准，依赖由 pnpm 与 Go 锁文件固定。治理规则中的版本约束待 G0 评审。

CI／`pnpm check` 已覆盖前端解析后的导入图、Go AST 边界、负向 fixture、合同、lint、类型、测试与构建；真实 PostgreSQL 套件独立验证 RLS、角色、原子事务和执行链路。该治理仍不等于完整语义纯度、所有动态 SQL 的数据所有权证明、生产秘密扫描或生成物漂移证明，后续继续按计划补齐。
