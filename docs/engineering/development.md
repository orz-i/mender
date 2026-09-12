# Mender 本地开发

## 环境与安装

使用 Node.js `24.19.0`、pnpm `11.18.0` 和 Go `1.26.7`。仓库中的 `.node-version`、`packageManager`、`engines`、`backend/go.mod` 与 `toolchain.versions.json` 必须同步维护。版本不一致时检查会失败，不自动替换系统工具链。

在仓库根执行：

```sh
pnpm install --frozen-lockfile
pnpm check:toolchain
```

Go 首次构建会根据 `go.sum` 下载模块，也可先在 `backend/` 执行 `go mod download`。默认探针／前端启动和普通检查不需要数据库；启用 Run 查询／取消需单独配置真实 PostgreSQL 与机器凭据。原合同包的 Python 校验脚本仅作为可选参考保留。

## 启动入口

每个长驻命令使用独立终端，Ctrl+C 停止：

| 命令（仓库根） | 服务 |
| --- | --- |
| `pnpm dev` | 同时启动 Console 5173 与 Admin 5174 |
| `pnpm dev:console` | 仅 Console |
| `pnpm dev:admin` | 仅 Admin |
| `pnpm dev:api` | API，默认绑定 `127.0.0.1:18080` |
| `pnpm dev:worker` | 空闲 Worker；尚无数据库或任务消费 |

Go 也可在 `backend/` 直接运行 `go run ./cmd/api` 或 `go run ./cmd/worker`。Console 与 Admin 的本地端口独立；Console OIDC/browser session、Workspace 选择和短时 Run delegation 已有受控 Alpha，公开注册/邀请仍未开放。Machine Key 只用于显式启用的服务/Agent 消费接口，不应粘贴进前端页面或聊天。

## 配置

API 读取进程环境变量，不自动读取 `.env`。默认无需配置。如需改变监听地址，PowerShell 中：

```powershell
$env:MENDER_HTTP_ADDR = '127.0.0.1:18081'
pnpm dev:api
```

在相应前端应用目录把 `.env.example` 复制为 `.env.local` 并修改 `MENDER_API_PROXY_TARGET`，或通过进程环境变量设置；修改后重启 Vite。默认代理目标为 `http://127.0.0.1:18080`，`/healthz`、`/readyz` 和预留的 `/api` 都通过开发服务器代理。该变量只供 Vite 服务端使用，不放入浏览器包。

`backend/.env.example` 是配置说明；前端两个应用的 `.env.example` 是 Vite 配置示例。真实本地配置已被 `.gitignore` 排除。

## 当前接口

| 路径 | 当前行为 |
| --- | --- |
| `GET /healthz` | `200`，`{"service":"mender-api","status":"ok","stage":"bootstrap"}` |
| `GET /readyz` | 默认 `503 not_ready`；显式启用的 Run 能力及其数据库／权限核验成功后就绪 |
| `POST /api/v1/workspaces/{workspace_id}/runs` | 默认未注册；启用后要求机器 Key 与 `run:create`，从已发布 Toolset/ToolVersion、主体 Connection 授权、PriceVersion 与当前 Budget period 解析不可变计划，再复用原子受理事务；当前 Job 仍保持 blocked |
| `GET /api/v1/workspaces/{workspace_id}/runs/{run_id}` | 默认未注册；启用后要求机器 Key 与 run:read scope |
| `GET /api/v1/workspaces/{workspace_id}/runs/{run_id}/artifacts` | Run read API 启用后注册；要求同一 Workspace 的 `run:read`，仅返回 immutable Artifact 元数据 |
| `GET /api/v1/workspaces/{workspace_id}/runs/{run_id}/artifacts/{artifact_id}` | Run read API 启用后注册；要求 `run:read`，返回 <=1 MiB 的 inline `application/json` content，不暴露 Provider control evidence |
| `POST /api/v1/workspaces/{workspace_id}/runs/{run_id}/cancel` | 默认未注册；要求 run:cancel；可选协调取消可原子停止未执行受理任务并释放原预留，其他路径保持本地取消／意图语义 |
| Worker SQL 租约／runtime host | Job/Attempt、lease/heartbeat/expiry/fencing 与 serial dispatch 已实现；ReviewedWorkerServices 可在同一有界 poll 周期显式调度 Provider Control/Reconciliation 和 Usage Settlement；默认 worker 未注入能力时仍不会访问供应商 |
| HTTP Supplier Submit / Provider status/cancel | hardened adapter、Broker、mounted SecretProvider 与 reviewed worker host 已实现；默认关闭，只有显式 master switch + 独立 DB roles + 精确 egress/provider allowlist 才可能访问供应商 |
| Usage Settlement | terminal Provider fact 会原子创建 pending settlement job；reviewed runtime 可通过 Worker host 每 Workspace 每周期最多 claim 一项；仍只是 quota settlement，不是 payment/revenue accounting |
| `POST /mcp/v1/workspaces/{workspace_id}` | 默认未注册；显式启用后只接受 MCP `2026-07-28` stateless Streamable HTTP，Bearer machine Key 在进入官方 SDK 前绑定 Workspace；暴露四个 `mender_*` 平台元工具 |
| 固定 Toolset direct MCP tools、上游 MCP Client、Agent、callback/webhook | 尚未开放 |

前端 `/` 提供 Alpha 导航，`/status` 发送真实健康请求；`/workspaces` 在 Console OIDC 启用后读取 HttpOnly server-side session 与当前 Workspace membership；`/connections` 使用独立 Connection manager 角色列出安全元数据、允许 Owner/Admin 撤销，并可启动一个服务端 reviewed OAuth Provider 的 PKCE 创建流程；`/runs` 通过显式短时 Human → Run delegation 读取/取消 Execution。OIDC Cookie 本身不能访问 Run API；delegation 只包含 `run:read` 与可选 `run:cancel`，没有 `run:create`，token 只保存在页面内存。Machine `/api/v1` 入口继续用于服务/Agent identity，Console 使用独立 `/api/console/v1` delegated surface。详见 [Console Run Explorer](2026-09-12-console-run-explorer.md)、[Workspace Console Alpha](2026-09-12-workspace-console-alpha.md)、[Connection self-service Alpha](2026-09-12-connection-self-service-alpha.md)、[Reviewed OAuth Connection Alpha](2026-09-12-connection-oauth-alpha.md) 与 [Human → Run delegation Alpha](2026-09-12-human-run-delegation-alpha.md)。

可选 `MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED=true` 后，`GET /api/console/v1/workspaces/{workspace_id}/launch-options` 通过当前 Human Membership + Workspace RLS 返回发布中的 Toolset/ToolVersion 与当前用户可用 Connection 的安全启动投影。它只暴露输入 Schema、Provider/Connection 标识、币种和 reserve quote，不暴露 credential ref、预算 ID/余额，也不等于 `run:create` 授权。详见 [Human launch discovery Alpha](2026-09-12-human-launch-discovery-alpha.md)。

进一步显式启用 `MENDER_CONSOLE_HUMAN_START_ENABLED=true` 后，Console 可以通过 Browser Session + CSRF mint 一次短时 Human StartRun delegation。该 token 精确绑定 Workspace、Toolset、ToolVersion、Connection、币种、费用上限与一个 Idempotency-Key；OIDC Cookie 本身仍不能访问 StartRun。`POST /api/console/v1/workspaces/{workspace_id}/runs` 使用独立 delegated authenticator，但复用 Machine StartRun 的同一 Admission Resolver/Unit of Work，因此 Schema、Connection、Price、Budget、幂等、预留、Run/Job/Outbox 规则没有前端或人类专用分叉。详见 [Human StartRun delegation Alpha](2026-09-12-human-start-run-delegation-alpha.md)。

### 可选持久化子集

保持 `MENDER_RUN_API_ENABLED=false` 时，不会创建数据库连接或迁移。启用时配置受限的 `MENDER_DATABASE_URL`；管理操作使用单独进程的 `MENDER_ADMIN_DATABASE_URL`。不得将迁移所有者或 superuser 配给 API。完整准备步骤、发行／撤销命令、回环测试库要求与限制见 [身份与 PostgreSQL 切片](2026-09-09-identity-postgres.md)。

`pnpm test:integration` 独立执行真实数据库套件；要求配置专用回环测试管理连接并设置 `MENDER_TEST_ALLOW_CREATE_DATABASE=true`，测试创建及清理自己的临时库／角色。缺少环境明确失败，不 Skip。也可在本机 Docker 已就绪时执行 `pnpm test:integration:docker`；该隔离套件已在协调取消收尾实际通过并清理自有资源。CI 服务已配置，但本轮不推送或宣称远程 CI 已运行。

公开 StartRun 还需 `MENDER_RUN_START_API_ENABLED=true` 和同库独立受限的 `MENDER_ADMISSION_DATABASE_URL`。已有角色使用 `pnpm db:grant-admission --role mender_admission` 授权；机器 Key 必须显式包含 `run:create`。API 读角色只读计划事实且不能读取 `connections.credential_version_ref` 或预算金额列；admission 写角色不能读取 Catalog/Connection/Price 源表。完整边界和测试证据见 [StartRun 收尾记录](2026-09-10-public-start-run.md)。

从 2026-09-12 起，已发布 ToolVersion 只要存在 `input_schema`，REST StartRun、`mender_run_start` 和 Fixed Toolset MCP 最终都由共享 Admission Resolver 在 quota 预留前验证 canonical business arguments。JSON Schema 具体实现留在 outbound adapter，且不会加载远程 `$ref`；Fixed Toolset MCP 不再拥有第二套业务 Schema 判定。历史没有 Schema 的 ToolVersion 暂保留兼容路径，后续发布治理应逐步消除该例外。

协调取消还需 `MENDER_RUN_COORDINATED_CANCEL_ENABLED=true` 和同库独立受限的 `MENDER_CANCELLATION_DATABASE_URL`。已有角色使用 `pnpm db:grant-cancellation --role mender_cancel` 授权，完整安全配置见[协调取消收尾记录](2026-09-10-coordinated-cancellation.md)。不要将管理连接或读角色复用为取消角色。

Worker 租约控制使用独立角色：先由管理员执行 `pnpm db:grant-worker --role mender_worker`，再给 Worker 进程配置 `MENDER_WORKER_DATABASE_URL`、稳定且非秘密的 `MENDER_WORKER_ID` 与显式 `MENDER_WORKER_WORKSPACES`。`MENDER_WORKER_CONTROL_ENABLED=true` 才连接控制数据库。若要求 dispatch，还需显式 deployment revisions、lease TTL、heartbeat interval 与 activation limit。`MENDER_WORKER_DISPATCH_ENABLED=true` 仍不能单独开启网络执行；必须同时启用 `MENDER_REVIEWED_WORKER_RUNTIME_ENABLED=true` 与 `MENDER_REVIEWED_HTTP_DISPATCH_ENABLED=true`，提供独立 executor role、mounted secret root 和精确 egress allowlist。当前 supervisor 单进程最多一个活动 submission。完整边界见 [Worker 租约记录](2026-09-10-worker-leases.md)、[HTTP Executor](2026-09-10-http-executor.md)、[Supplier Dispatch Supervisor](2026-09-10-dispatch-supervisor.md)与 [Runnable Alpha 第二批](2026-09-12-runnable-alpha-second-slice.md)。

从 0006 升级到 0007 时，除了新建/授权 worker role，还要对既有 admission role 再执行一次 `pnpm db:grant-admission --role <role>`，以收窄历史整表 Job INSERT ACL；不应只迁移 schema 后直接重启 StartRun API。

应用 `0008_supplier_submission.sql` 后，已有 worker role 必须再次执行 `pnpm db:grant-worker --role <role>`。应用层此时具备 durable intent、accepted provider IDs、unknown/reconciling 与 lease-expiry no-blind-retry 协议，但 `cmd/worker` 仍不会 activation/lease，也没有 HTTP/MCP/Agent supplier adapter；详情见 [供应商提交协议](2026-09-10-supplier-submission.md)。

应用 `0013_provider_control_endpoints.sql` 后，Supply deployment 可以声明固定 HTTP status/cancel POST endpoint。Provider request/task handle 始终作为有界 JSON body 发送，不拼入 URL。reviewed worker host 只接受 executor/reconciler 两个独立角色、Provider ID allowlist、精确 egress allowlist 与 mounted SecretProvider；环境里没有 provider endpoint 或 secret 值。`MENDER_REVIEWED_PROVIDER_CONTROL_ENABLED=true` 只有在 reviewed master switch 和 worker Workspace scope 同时存在时才生效。实现与验证边界见 [Provider HTTP Control Runtime](2026-09-11-provider-control-runtime.md)、[Runnable Vertical Alpha Foundation](2026-09-12-runnable-alpha-foundation.md) 与 [Runnable Alpha 第二批](2026-09-12-runnable-alpha-second-slice.md)。

应用 `0014_usage_settlement_contract.sql` 与 `0015_terminal_settlement_jobs.sql` 后，既有固定价格被明确为 `fixed_success_only`，Provider 终态收敛会在同一 Execution 事务创建 settlement job。先由管理员执行 `pnpm db:grant-settlement --role <role>` 为预先存在的独立角色授权；reviewed worker host 可通过 `MENDER_REVIEWED_SETTLEMENT_ENABLED=true` 显式组合该 runtime。每轮对每个配置 Workspace 最多 claim 一个 settlement job；结算事务不执行 Provider、Secret、Webhook 或支付网络调用。实现与验证边界见 [Commerce Usage Settlement](2026-09-11-commerce-usage-settlement.md) 与 [Runnable Alpha 第二批](2026-09-12-runnable-alpha-second-slice.md)。

MCP gateway 使用官方 `github.com/modelcontextprotocol/go-sdk` v1.7.0，并把本阶段兼容承诺固定为 `2026-07-28`。启用 `MENDER_MCP_GATEWAY_ENABLED=true` 前还必须同时启用 Run API、Run read、StartRun 和 coordinated cancellation，并配置现有 reader/admission/cancellation 角色及 cursor signing key。入口是 `/mcp/v1/workspaces/{workspace_id}`，当前工具只有 `mender_run_start`、`mender_run_get`、`mender_run_cancel`、`mender_artifact_get`。StartRun 的 `arguments` 从 SDK raw `json.RawMessage` 严格解析后交给现有 Admission，避免大整数在幂等哈希前转换为浮点；客户端断开不会撤销已持久受理的 Run，应复用同一 `idempotency_key` 查询结果。详情见 [MCP Meta-Tool Gateway](2026-09-11-mcp-meta-tool-gateway.md)。

应用 `0016_execution_artifacts.sql` 后，已有 API runtime 与 reconciler role 都要重新执行对应 grant。Runtime 只新增 Artifact 安全列读取，明确看不到 `source_observation_id`；Reconciler 只增加 Artifact SELECT/INSERT，以便 succeeded Provider Observation 与结果 Artifact 在同一 Execution terminal transaction 中提交。`MENDER_RUN_READ_API_ENABLED=true` 时 Artifact 路径随 Run read handler 一起注册，没有匿名结果 API，也没有对象存储配置。实现、历史回填和真实 PostgreSQL 证据见 [Artifact / Result Foundation](2026-09-11-artifact-result-foundation.md)。

## 构建与检查

```sh
pnpm check
```

需要缩小检查范围时，使用 [脚本说明](../../scripts/README.md)。`pnpm build` 产物分别位于 `frontend/apps/console/dist` 和 `frontend/apps/admin/dist`。共享包直接导出源码，由 Vite 构建，无需提前生成共享 JS 制品。

`pnpm build:backend` 编译所有 Go 包以验证；需要可执行文件时在 `backend/` 执行：

```sh
go build -o bin/ ./cmd/api ./cmd/worker
```

`pnpm --filter @mender/console preview` 和 Admin 的同名命令仅预览本地构建，默认端口为 6173／6174。部署时静态站点需要配置 SPA fallback 和同源 API 反向代理；仓库没有生产部署配置或授权流程。

## 常见情况

- 默认 `/readyz` 返回 503 是预期状态；启用后的 200 只表示已配置的 API Run 查询／创建／取消依赖可用。Worker 使用独立启动与数据库角色核验，不纳入 API readiness；两者都不代表供应商或支付链路就绪。
- 页面连接失败时先检查 API 终端、监听端口和代理目标，再点“重新检查”。
- 端口冲突会直接报错，不自动切到其他端口。修改配置或结束自己启动的旧进程。
- 工具链检查失败时安装清单指定版本；升级需一起调整配置、真实锁文件与验证证据。
- pnpm 拒绝过新的或未获准构建脚本的依赖时，检查具体依赖和发布元数据；不使用全量豁免作为默认安装方式。

CI 文件已创建但未在远程运行。原设计的完整架构、安全、资金和协议门禁见 G07，当前基础检查不能替代这些工作。
