# Mender 检查入口

新增原子受理验证：`pnpm test:integration:docker` 使用自有临时 PostgreSQL 执行幂等受理、额度竞争、六个写入点故障回滚和已受理 Run 取消保护；`node scripts/backend.mjs test -race -count=1 ./tests/admission ./internal/contexts/commerce/...` 验证领域／用例。内部端口尚未装入公开 StartRun，完整边界见 [实现记录](../docs/engineering/2026-09-09-atomic-admission.md)。

均从仓库根使用 pnpm：

| 命令 | 验证范围 |
| --- | --- |
| `pnpm check` | 下列必要检查和构建的聚合入口，任一失败即停止 |
| `pnpm test:integration:docker --check` | 仅检查本地 Docker 条件；不创建数据库，不视为集成通过 |
| `pnpm test:integration:docker [--pull]` | 使用自有临时 PostgreSQL 容器运行真实套件并清理；默认不下载镜像 |
| `pnpm check:toolchain` | Node／pnpm／Go 精确版本与清单一致 |
| `pnpm check:workspace` | 必需包、scripts、workspace: 依赖、唯一锁文件 |
| `pnpm check:docs` | 计划计数、任务引用和 Markdown 本地链接 |
| `pnpm check:contracts` | Schema、样例、制品摘要、OpenAPI 结构 |
| `pnpm check:architecture` | TypeScript 真实模块解析、前端分层／公开入口／源码循环，以及 Go AST 依赖规则与负向 fixture |
| `pnpm lint` | JS／TS／React 语法和基础规则 |
| `pnpm typecheck` | 工作区严格 TypeScript 检查 |
| `pnpm test` | 健康传输测试，以及 TypeScript 边界、别名、type-only、re-export、动态加载和循环的负向样例 |
| `pnpm test:backend` | 探针／生命周期、Go 边界、Run、机器身份、HTTP 与配置单元测试；不执行 integration 标签 |
| `pnpm test:integration` | 独立真实 PostgreSQL 套件；缺少专用测试连接与创建数据库授权即失败，不 Skip |
| `pnpm vet:backend` | Go vet |
| `pnpm build:backend` | Go 全包编译 |
| `pnpm build` | Console／Admin 独立构建 |

脚本只校验其声明的范围，不将 AST／导入扫描当作完整语义纯度或数据库所有权证明。前端使用现有 TypeScript Compiler API 解析实际 tsconfig 和模块路径；检查代码不新增第三方依赖。Go 检查直接依赖与部分明显 I/O，不能证明任意调用链没有副作用。

定向运行本轮测试：

```sh
node scripts/backend.mjs test -count=1 ./internal/contexts/execution/... ./tests/execution ./tests/architecture
node scripts/backend.mjs test -race -count=1 ./internal/contexts/execution/... ./tests/execution
```

race 检查要求对应宿主工具链支持；它不等于数据库或分布式并发验证。覆盖边界及后续工作见 [implementation 记录](../docs/engineering/2026-09-09-execution-foundation.md)。

本轮可增加 `./tests/identity ./tests/httpapi ./internal/platform/postgres ./internal/bootstrap` 的定向验证。`pnpm check` 不需要数据库；CI 在其后使用专用 PostgreSQL 服务单独运行 `pnpm test:integration`，未完成真实套件不得声称持久化已验收。

操作员写命令：`pnpm db:migrate`、`pnpm db:grant-runtime --role <role>`、`pnpm key:issue --workspace <workspace> --subject <service-account>`、`pnpm key:revoke --id <key-id>`。这些不是检查命令，需要安全注入管理连接；不要在普通 CI 或未授权数据库运行。发行 Key 只在持久提交后打印一次秘密，不能公开日志。详见 [身份与 PostgreSQL 记录](../docs/engineering/2026-09-09-identity-postgres.md)。

`backend.mjs` 只使用无 shell 的子进程调用 Go，便于 Windows 与 Linux 共享命令；它不运行安装器，也不自动切换 Go 版本。

列表／时间线与隔离测试入口的细节见 [Run 只读模型记录](../docs/engineering/2026-09-09-run-read-models.md)。`pnpm test` 中的编排测试使用 Fake Docker runner，只验证控制流与安全边界，不提供真实 PostgreSQL 通过证据。`pnpm check` 不自动执行需要外部服务的集成套件；CI 单独运行该步骤。
