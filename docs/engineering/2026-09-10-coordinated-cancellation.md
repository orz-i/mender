# Mender：协调取消与额度释放收尾

日期：2026-09-10。保留固定工具链、依赖锁及此前工作树。本阶段只处理能够确认尚未执行的内部受理任务，不开放公共 StartRun、真实 Worker、供应商调用或支付。

## 已实现边界

commerce 在原预算期释放 held 预留；execution 在同一事务停止 blocked Job、将 queued v1 Run 变为 canceled v2、保存 RunEvent、抑制原 run.admitted Outbox、追加 run.canceled Outbox 与不可变取消回执。应用层消费本地端口，bootstrap 在同一 pgx 事务上装配各上下文能力，各域只写自己的表。

新增 `0005_coordinated_cancellation.sql`，未改写 0001–0004。延迟总和约束维持 held 预留合计；延迟外键及 execution 自有检查要求释放与取消事实匹配。此为受控同库耦合，不代表 ADR-020 已获正式批准。

锁顺序为原预算期、预留、Run、Job、原受理 Outbox。锁前授权，等待锁后再次验证当前凭据、Workspace 和 run:cancel 权限。事务内不请求上游、不支付、不投递外部消息。

| 场景 | 行为 |
| --- | --- |
| queued v1、blocked／executor_not_configured、held 预留、pending 受理事件 | 全部取消／释放事实原子提交后返回成功 |
| 重复取消 | 重新授权并核对回执与释放证据，返回原结果；不重复释放，不覆盖首次原因 |
| running、reconciling 或状态不一致 | 失败关闭，额度保持不变 |
| 原预算期过期或 inactive | 只释放原预留，不增加新周期额度 |
| 提交结果未确认 | 不返回成功；恢复后重试同一 Run，通过持久回执确认 |
| 非受理关联的旧 Run | 保留原 CAS 取消；协调器错误不能降级到旧路径绕过 |

创建幂等重放仍返回原受理事实，不能重新创建已取消任务或再次占额度。当前额度不是钱包、收入或复式账本；释放不等于支付退款。Outbox 尚无投递器，Job 仍不能被真实 Worker 领取。

## 接口与配置

沿用 `POST /api/v1/workspaces/{workspace_id}/runs/{run_id}/cancel`。JSON 对象中 reason 可选、最多 500 字符，正文最多 4096 字节。合同 `contracts/run-query-cancel.openapi.yaml` 更新为 v0.3.0。

| 响应 | 含义 |
| --- | --- |
| 200 | 已提交协调取消／授权重放，或旧 Run 本地取消／原终态 |
| 202 | 旧非托管任务的取消意图，不表示上游停止 |
| 401／403／404 | 凭据失效、权限拒绝、对象不存在 |
| 409 ADMISSION_CANCEL_UNAVAILABLE | 未启用协调器，旧路径拒绝单独修改带预留任务 |
| 409 UNSAFE_CANCELLATION | 不能证明未执行，不释放额度 |
| 503 CANCELLATION_COMMIT_UNCONFIRMED | 提交未确认，恢复后重试同一个 Run |

默认关闭。需显式设置 `MENDER_RUN_API_ENABLED=true` 和 `MENDER_RUN_COORDINATED_CANCEL_ENABLED=true`，并安全注入读角色 `MENDER_DATABASE_URL` 与取消角色 `MENDER_CANCELLATION_DATABASE_URL`。两连接配置须为同一数据库端点／库名、不同受限角色。启动验证迁移和权限，失败不降级。取消角色不能创建 Run、读取受理参数或修改身份。

```sh
pnpm db:migrate
pnpm db:grant-runtime --role mender_app
pnpm db:grant-cancellation --role mender_cancel
```

以上供操作员在指定开发数据库显式运行，要求独立安全注入 `MENDER_ADMIN_DATABASE_URL`。角色须预先由管理员创建；命令不创建账户、不打印密码，只应用既定权限，API 启动仍检查是否过宽。不要将管理连接配置给 API。`.env.example` 只是示例，API 不自动加载 .env。本轮没有操作持久开发库，只执行自有隔离测试资源。

## 中断恢复与测试修正

上次两个真实库 fixture 的容器时间可能早于宿主写入时间。修正为 `GREATEST(updated_at, clock_timestamp())` 和 `GREATEST(created_at, clock_timestamp())`，没有放宽生产时间／RLS／事务约束。

四个新增测试在连接中断后首次实际运行发现一个错误名引用，已把不存在的 `ports.ErrCancelUnsafe` 修正为真实合同 `ports.ErrUnsafeCancel`，没有改生产错误语义或移除断言。

本轮新增的操作员 grant-cancellation 路径在真实隔离库测试中执行，并检查无秘密输出、重复授予和受限角色验证。纯用例测试只证明步骤与端口行为，不替代实际 SQL 回滚或并发证据。

## 验证

```sh
node scripts/backend.mjs test -race -count=1 ./tests/admission ./tests/execution ./tests/httpapi ./tests/architecture ./internal/contexts/commerce/... ./internal/bootstrap
pnpm test:integration:docker
pnpm check
git diff --check
```

真实套件覆盖 16 个并发重复取消只释放一次、八个写入点逐个失败全回滚、原周期释放、危险状态拒绝、取消与 16 个新受理竞争、缺少匹配取消记录不能提交、真实 HTTP 身份与专用角色、连接池租户上下文清理。临时角色和容器由套件创建并清理，不接触生产。

### 2026-09-10 收尾实证

| 检查 | 本轮实际结果 |
| --- | --- |
| 非缓存 Go race | exit 0；admission、execution、httpapi、architecture、commerce domain 与 bootstrap 通过，包含上次未执行的四个新测试文件 |
| `pnpm test:integration:docker` | exit 0；真实 PostgreSQL 套件 1.728s，包含协调取消、并发、八写点回滚、旧周期、权限和新操作员授予入口；自有容器已清理 |
| 最终 `pnpm check` | exit 0；工具链、workspace、文档、合同、Go／TS 边界、lint、typecheck、测试、vet、后端和双前端构建通过 |

全量检查还发现 YAML 流式响应中带逗号的 description 被解析为多个映射键；已补充引号，新增响应键检查，并重跑同一全量入口通过，没有移除断言或把失败标为预期。纯用例／Mock 验证与真实 PostgreSQL 证据分开记录；`pnpm check` 本身不运行真实数据库套件，后者已单独执行。

未提交、推送或部署；不修改原 WBS／ADR／G0–G5 为正式批准。本轮没有新增业务前端或执行浏览器验收。最终文档与差异检查由收尾命令另行复核，证据写入当前 Session。
