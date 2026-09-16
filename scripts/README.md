# Mender 检查入口

当前实现与剩余验收以[统一状态页](../docs/planning/current-status.md)为准。下方带日期记录属于历史验证说明，不能据早期“尚未接入”描述否定后来公共 StartRun。`pnpm test:integration:docker` 使用自有临时 PostgreSQL 验证持久化与故障恢复；是否通过必须以实际运行记录为准。

均从仓库根使用 pnpm：

| 命令 | 验证范围 |
| --- | --- |
| `pnpm check` | 下列必要检查和构建的聚合入口，任一失败即停止 |
| `pnpm test:s4:tools` | CLI／有界质量任务／真实Ed25519签名的单元和合同测试 |
| `pnpm test:s4:recovery` | 构建后在自有临时容器执行真实PG／CLI／Chromium及pg_dump/pg_restore对账，结束清理 |
| `pnpm check:dependencies` | 在线pnpm advisory、Go模块完整性及固定govulncheck；网络不可用/可达漏洞不视为通过 |
| `pnpm cli --help` | 消费既有MCP端点的CLI，不授予权限；执行需明确--execute，参数走stdin |
| `pnpm quality:probe CONFIG.json` | 固定基线和明确端点的只读MCP发现合成采样，失败非零且不覆盖基线 |
| `pnpm quality:report REPORT.json` | 输出无脚本/CSP只读质量视图，不展示秘密或作发布批准 |
| `pnpm release:artifact ...` | 显式清单、真实Ed25519签名及独立可信公钥验证，不生成生产密钥或部署 |
| `pnpm test:integration:docker --check` | 仅检查本地 Docker 条件；不创建数据库，不视为集成通过 |
| `pnpm test:integration:docker [--pull]` | 使用自有临时 PostgreSQL 容器运行真实套件并清理；默认不下载镜像 |
| `pnpm check:toolchain` | Node／pnpm／Go 精确版本与清单一致 |
| `pnpm check:workspace` | 必需包、scripts、workspace: 依赖、唯一锁文件 |
| `pnpm check:docs` | 计划计数、任务引用和 Markdown 本地链接 |
| `pnpm check:status` | 原任务集合、历史别名、实施／验证／验收分离、证据摘要、生成状态页与范围提升回执 |
| `pnpm status:render` | 审阅并修改主台账后重新生成当前状态 Markdown，不改变任务状态或批准验收 |
| `pnpm check:contracts` | Schema、样例、制品摘要、OpenAPI 结构 |
| `pnpm check:architecture` | TypeScript 真实模块解析、前端分层／公开入口／源码循环，以及 Go AST 依赖规则与负向 fixture |
| `pnpm lint` | JS／TS／React 语法和基础规则 |
| `pnpm typecheck` | 工作区严格 TypeScript 检查 |
| `pnpm test` | 显式列举的 Node／API 客户端测试及状态、证据、架构负向样例；不等于浏览器或真实数据库验收 |
| `pnpm test:backend` | 探针／生命周期、Go 边界、Run、机器身份、HTTP 与配置单元测试；不执行 integration 标签 |
| `pnpm test:integration` | 独立真实 PostgreSQL 套件；缺少专用测试连接与创建数据库授权即失败，不 Skip |
| `pnpm vet:backend` | Go vet |
| `pnpm build:backend` | Go 全包编译 |
| `pnpm build` | Console／Admin 独立构建 |

脚本只校验其声明的范围，不将 AST／导入扫描当作完整语义纯度或数据库所有权证明。前端使用现有 TypeScript Compiler API 解析实际 tsconfig 和模块路径；检查代码不新增第三方依赖。Go 检查直接依赖与部分明显 I/O，不能证明任意调用链没有副作用。

状态检查是阻断式配置一致性检查，不是新增产品阶段。`node --test scripts/project-status.test.mjs` 使用内存副本覆盖虚报完成、遗失任务、别名扩张、摘要漂移、目录穿越、生成页手改及将 sandbox／CI 成功当外部批准的反例；不会改写真实台账或创建真实批准回执。它仍不能证明人工签字身份、外部 URL 或远端分支保护实时有效，详见[状态维护约定](../docs/planning/status-tracking.md)与[合并门禁](../docs/engineering/merge-gate.md)。

定向运行本轮测试：

```sh
node scripts/backend.mjs test -count=1 ./internal/contexts/execution/... ./tests/execution ./tests/architecture
node scripts/backend.mjs test -race -count=1 ./internal/contexts/execution/... ./tests/execution
```

race 检查要求对应宿主工具链支持；它不等于数据库或分布式并发验证。覆盖边界及后续工作见 [implementation 记录](../docs/engineering/2026-09-09-execution-foundation.md)。

本轮可增加 `./tests/identity ./tests/httpapi ./internal/platform/postgres ./internal/bootstrap` 的定向验证。`pnpm check` 不需要数据库；当前CI在其后运行独立依赖扫描，再显式安装Chromium并运行 `pnpm test:integration:browser`（MENDER_S4_BROWSER=true），缺少测试库/浏览器条件失败而不跳过。本地自有容器入口还可增加 `--recovery`，真实恢复演练不是普通CI服务步骤的隐含保证。

原S4本地工程与G4边界见[收口记录](../docs/engineering/2026-09-16-s4-closeout.md)。`node scripts/record-s4-verification.mjs MODE --record`只执行固定命令并记录实际输出与源码指纹；`node scripts/review-s4-closeout-status.mjs --receipts`只在实际回执及文件摘要一致时回填原任务的验证轴，不授予任务或Gate验收。

操作员写命令：`pnpm db:migrate`、`pnpm db:grant-runtime --role <role>`、`pnpm db:grant-admission --role <role>`、`pnpm db:grant-cancellation --role <role>`、`pnpm db:grant-worker --role <role>`、`pnpm db:grant-executor --role <role>`、`pnpm db:grant-mcp-connector --role <role>`、`pnpm db:grant-reconciler --role <role>`、`pnpm db:grant-settlement --role <role>`、`pnpm key:issue --workspace <workspace> --subject <service-account> --scopes run:read,run:create`、`pnpm key:revoke --id <key-id>`。这些不是检查命令，需要安全注入管理连接；不要在普通 CI 或未授权数据库运行。Worker 只负责本地租约/submission；executor-runtime 只读 HTTP 执行材料；MCP connector 额外拥有 Supply-owned snapshot/route/result evidence 的最小权限；Cancellation role 可写用户 provider-cancel intent 但不能写远端 outcome；Reconciler 只写 provider observation、claim/resolve cancel intent 与终态收敛。SecretProvider 仍是独立运行时能力。发行 Key 只在持久提交后打印一次秘密，不能公开日志。详见 [Supplier Runtime Broker](../docs/engineering/2026-09-10-supplier-runtime-broker.md)、[Upstream MCP Client Adapter Foundation](../docs/engineering/2026-09-11-upstream-mcp-client-foundation.md)、[Provider Result Lifecycle](../docs/engineering/2026-09-10-provider-result-lifecycle.md)和 [Provider Cancellation](../docs/engineering/2026-09-10-provider-cancellation.md)。

`backend.mjs` 只使用无 shell 的子进程调用 Go，便于 Windows 与 Linux 共享命令；它不运行安装器，也不自动切换 Go 版本。

列表／时间线与隔离测试入口的细节见 [Run 只读模型记录](../docs/engineering/2026-09-09-run-read-models.md)。`pnpm test` 中的编排测试使用 Fake Docker runner，只验证控制流与安全边界，不提供真实 PostgreSQL 通过证据。`pnpm check` 不自动执行需要外部服务的集成套件；CI 单独运行该步骤。
