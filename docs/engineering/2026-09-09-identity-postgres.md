# Mender：机器身份、PostgreSQL 与受保护 Run 接口

后续进展：此处原测试限制是该阶段历史记录。当前 Docker 已启动，真实 PostgreSQL 迁移／权限／RLS／CAS／回滚套件已重跑通过，见 [原子受理记录](2026-09-09-atomic-admission.md)。

日期：2026-09-09。沿用 Node 24.19.0、pnpm 11.18.0、Go 1.26.7。新增锁定 PostgreSQL 驱动 `github.com/jackc/pgx/v5 v5.11.0`，只调整 Go 模块及其校验文件；前端锁和工具链不变。

## 实现范围

本轮在已有 Run 聚合和架构检查之上增加机器凭据认证、Workspace 与 scope 授权、PostgreSQL 仓储、迁移、受保护查询／取消 HTTP，以及显式 bootstrap。它不是人类用户登录、完整 RBAC、OAuth、付费受理或真实 Worker 执行闭环。

| 层次 | 当前代码 |
| --- | --- |
| identity domain / application | Credential 有效期、禁用、撤销与 scope；消费方 Codec／Repository／Clock 端口 |
| identity 外层 | 高熵 Key 编解码、PostgreSQL 查询及操作员发行／撤销；公开 facade |
| execution 防腐层 | 仅消费 identity/public；映射为本地 Caller、认证与授权端口 |
| execution 持久化 | Workspace＋Run 复合查询、受校验 Restore、版本 CAS、同事务追加操作者事件 |
| HTTP 与 bootstrap | 两条受保护路由；默认不启用；数据库配置和权限失败时不降级 |
| migrations / operator | 显式执行迁移、授予已有受限角色权限、发行及撤销 Key |

### 机器身份与授权

Key 格式为 `mender_live_<32 位十六进制 ID>.<43 字符随机 secret>`，secret 来自 32 字节密码学随机数。数据库保存 SHA-256 校验摘要，不保存原始 Key；摘要比较使用固定时序比较。此设计用于随机机器凭据，不适用于人类密码存储。

HTTP 只接受单个 Authorization Bearer 值。不采信 X-Workspace-ID、X-Subject-ID 或请求 JSON 中的主体。通过认证形成可信 Caller 后，校验路径 Workspace；应用用例再次查询 Key、服务账号及工作区状态，验证 `run:read` 或 `run:cancel`，然后才能访问 Run。未实现正向授权缓存；撤销不承诺追溯阻断已经完成授权的在途请求。

目前一个机器凭据绑定一个 Workspace。指定的 scope 适用于该 Workspace 内的 Run，不代表已经实现细粒度 Toolset、成员角色、对象级分享或平台超级管理员。Key 发行是本地受信操作员能力，不是公开注册接口。禁用的工作区或账号不会因再次发行 Key 被自动激活。

### 持久化与事务

`identity` 拥有工作区、服务账号及 Key 表；`execution` 拥有 Run 与 `run_events`。跨上下文只经公开合同访问身份，不让 execution 仓储写 identity 表。

每次 Run 查询／保存开启短事务，以 `set_config(..., true)` 设置事务级 Workspace。Run 表和事件表都配置 ENABLE＋FORCE RLS，且 SQL 同时带 Workspace 条件。RLS 是纵深防御，不代替应用认证，也不把有任意 SQL 执行能力的已攻陷应用变成可信租户隔离边界。

保存使用 expectedVersion CAS，保护 ID／创建时间、版本增长及更新时间。状态变更与包含 subject_id、credential_id、reason 的事件在同一事务提交；事件写入失败时状态更新回滚。事件是 execution 的持久变更记录，不是已实现的跨服务 Outbox，也不代表完整平台安全审计。

取消排队任务可落为 canceled；运行中取消只记录 cancel_requested。尚无 Worker 读取取消意图并向上游发送取消请求。对账中任务继续返回明确的结果未确认错误，不擅自释放预算。

## 接口合同与默认行为

窄合同在 [run-query-cancel.openapi.yaml](../../contracts/run-query-cancel.openapi.yaml)，与仍处于设计阶段的完整消费 API 分开。

| 路径 | 条件与结果 |
| --- | --- |
| GET /healthz | 默认和启用后均表示进程存活，不证明业务完整 |
| GET /readyz | 默认 503；显式启用且迁移、角色和依赖检查成功后 200，stage 为 run-query-cancel |
| GET /api/v1/workspaces/{workspace_id}/runs/{run_id} | 机器认证、Workspace 与 run:read 权限通过后查询 |
| POST 同一 Run 路径 /cancel | application/json 对象；可选 reason；取消意图持久化 |
| POST /api/v1/workspaces/{workspace_id}/runs | 未开放，不提供新建或付费执行 |

成功响应只包含已知的执行状态、版本字符串、ID 与时间，不伪造 billing_state、余额或 trace。未知字段、重复 JSON 键、大小写字段绕行、null reason、超长原因、尾随 JSON 被拒绝；正文上限 4096 字节。错误只返回稳定分类，不携带 DB DSN、秘密或原始驱动错误。响应标记 no-store，request_id 由服务端生成。

`/readyz` 的 200 只覆盖已启用的 Run 查询／取消子集，不是 MCP、Agent、计费、Worker 或商业上线已就绪。

## 操作员与运行账号隔离

API 进程只读取 `MENDER_DATABASE_URL`，不使用管理连接。操作员进程只读取 `MENDER_ADMIN_DATABASE_URL`。运行角色必须非 superuser、非 BYPASSRLS、无创建角色／数据库权限、不是受保护 schema／表所有者，且不能修改 identity、迁移元信息或 Run 不可变列。

首次接入由数据库管理员创建业务数据库和迁移所有者，并建立独立的运行登录角色，例如：

```sql
CREATE ROLE mender_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
```

通过数据库管理员的安全凭据渠道设置该角色密码，不把密码写入仓库、聊天或命令记录。`grant-runtime` 仅向已存在角色增加该子集必要权限，不清除历史的过宽授权；API 启动检查发现过宽授权仍会拒绝启动。迁移所有者也不应用作 API 运行用户。

完成安全注入管理连接后，从仓库根执行：

```sh
pnpm db:migrate
pnpm db:grant-runtime --role mender_app
pnpm key:issue --workspace ws_local --subject sa_local --scopes run:read,run:cancel --ttl 24h
```

发行只在数据库提交成功后将原始 Key 写到当前操作员命令输出一次；妥善保存输出，不放入 CI 日志或公开终端记录。若提交成功但输出丢失，撤销该 Key 后重新发行，而不是提供明文取回接口。撤销只传 Key ID：

```sh
pnpm key:revoke --id <key_id>
```

普通应用进程通过安全环境配置 `MENDER_RUN_API_ENABLED=true` 与受限的 `MENDER_DATABASE_URL` 后启动。未配置开关时维持 probes-only；开关拼写错误、无 DSN、连接失败、迁移缺失或角色不合规时失败关闭，不自动使用内存存储。

本机回环 PostgreSQL 可用于受控开发连接；非回环 PostgreSQL 要求 verify-full 等已验证 TLS，并拒绝不安全 fallback。API 对外服务还需受控入口、HTTPS、速率／连接限制与安全审查；本轮不是公网部署授权，默认监听仍为回环地址。

迁移在一个显式操作员事务中执行，使用锁和内容摘要；重复执行验证一致性，拒绝已登记迁移漂移。摘要归一化 CRLF/LF，避免 Windows／Linux 换行差异。当前版本严格要求匹配迁移集，尚不是支持任意新旧版本混跑的滚动迁移系统。不要修改已应用迁移或手工改摘要；后续采用新增迁移和单独发布评审。

API 启动不执行 DDL、不建账号、不灌入示例 Run。现阶段没有 StartRun，用例演示数据只能存在隔离测试数据库；不能为演示业务可用而向真实库注入伪任务。

## 验证与环境限制

单元／HTTP 测试使用明确的测试仓储验证实际认证、facade、防腐层、用例和 Gin 链路。测试覆盖 malformed／unknown Key、错误 secret、过期、撤销、禁用、scope 限制、跨 Workspace、拒绝前不读 Run、严格请求体、取消意图、错误脱敏、配置失败与历史状态矩阵。

```sh
pnpm check
node scripts/backend.mjs test -count=1 ./tests/identity ./tests/httpapi ./tests/execution ./tests/architecture ./internal/contexts/execution/domain ./internal/platform/postgres ./internal/bootstrap
node scripts/backend.mjs test -race -count=1 ./tests/identity ./tests/httpapi ./tests/execution ./internal/contexts/execution/domain
git diff --check
```

真实 PostgreSQL 套件单独执行：

```sh
pnpm test:integration
```

需预先配置 `MENDER_TEST_DATABASE_URL`，指向专用回环测试服务器的管理数据库，并显式设置 `MENDER_TEST_ALLOW_CREATE_DATABASE=true`。测试会创建随机命名的临时数据库和受限角色，最终只删除自己创建的资源；需要相应 CREATE DATABASE／ROLE 权限，禁止连接生产服务器。它验证迁移重复执行／漂移、受限角色、RLS、连接池租户上下文清理、真实并发 CAS、事件失败回滚和实际 PostgreSQL＋HTTP 链路。

本轮宿主 Docker 守护进程不可用，没有配置可用测试数据库。集成测试已编译，缺少条件时实际以 exit 1 明确失败，没有 Skip；这仅验证前置条件保护，**不能称为真实 PostgreSQL 套件通过**。CI 已配置 PostgreSQL 18.6 服务及独立执行步骤，但没有提交／推送，远程 CI 未运行。本阶段数据库持久性、RLS 与事务验收保持待验证，必须通过真实套件后才能作为已验收能力对外使用。

## 未包含

### 本轮已执行的检查记录

| 检查 | 2026-09-09 本机结果 |
| --- | --- |
| 最终 `pnpm check` | exit 0；工具链、工作区、文档、合同、架构、lint、类型、Node／Go 测试、vet、后端与双前端构建通过 |
| identity／HTTP／Run／配置／架构定向非缓存测试 | exit 0；包括撤销、过期、scope、跨 Workspace、取消状态与错误脱敏 |
| identity／HTTP／Run `go test -race -count=1` | exit 0；这是进程内竞争检查，不是数据库并发验收 |
| `pnpm test:integration`，测试连接配置为空 | 实际 exit 1；按设计拒绝缺失环境。套件编译通过，但真实数据库测试体未执行 |
| GitHub Actions PostgreSQL 服务 | 已写入配置，未推送／未在远程执行 |

上表属于本次开发验证记录，不更改 WBS、ADR 或阶段 Gate 的正式验收状态。

未实现人类登录／OIDC、成员权限管理界面、Key 自助管理页面、完整授权策略、跨服务 Outbox、取消投递 Worker、StartRun、预算预留、费用结算、MCP／Agent 调用、支付或部署。现有前端页面保持不变。

设计文档快照、WBS、ADR-020 与 G0–G5 不因本轮代码和单元检查而标为验收完成。后续优先准备专用 PostgreSQL 并执行本套件；通过后再继续账户交互和持久任务受理，不跳过资金事务门禁。

## 一手资料

- pgx v5.11.0 与事务／连接池接口：https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0/pgxpool
- pgx 官方发行记录：https://github.com/jackc/pgx/releases
- PostgreSQL RLS 与 owner／BYPASSRLS 边界：https://www.postgresql.org/docs/current/ddl-rowsecurity.html

这些资料用于实现参考，不代替本项目真实数据库测试结果。
