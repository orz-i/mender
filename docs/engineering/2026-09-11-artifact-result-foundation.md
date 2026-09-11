# Mender：Artifact / Result Foundation

日期：2026-09-11。该阶段承接已经完成的 Provider terminal result、Control Runtime 与 Usage Settlement，目标是把供应商内部的 `provider_observations.result_json` 转化为稳定、面向消费者的 Artifact 合同，为后续 Console、MCP 和 Agent 结果映射提供统一结果入口。

## Stage 1：immutable provider_result Artifact

Execution 新增 `Artifact` 领域类型和 `0016_execution_artifacts.sql`。首版只接受 `provider_result` / `application/json`，大小沿用 Provider Observation 的 1 MiB 上限。Artifact ID 为稳定的 `art_<run_id>`；同一 Workspace/Run 只能存在一个 `provider_result` Artifact。

Provider `succeeded` 收敛现在在同一 Execution 事务内完成：Provider Observation → Artifact → settlement job → finished Job → succeeded Run。failed/canceled/pending 不产生成功结果 Artifact。数据库使用 deferred bundle trigger 双向证明 succeeded Observation 与 Artifact 的 source observation、JSON 内容和时间一致；任何单独插入或冲突内容在提交时失败。

迁移会把升级前已经存在的 succeeded Provider Observation 回填为 Artifact，因此已有成功 Run 不会因升级后查询新结果入口而缺失。Artifact 与 Provider Observation 都保持不可变；本阶段没有对象存储搬迁、文件上传或签名 URL。

Reconciler 仍是最小的 Execution 结果写入 principal。为了在终态事务内创建 Artifact，它获得 `execution.artifacts` 的 SELECT/INSERT，但没有 UPDATE/DELETE；现有对 Provider Observation 的读写边界没有扩大到 Identity、Connections、Supply、Commerce 或 canonical arguments。

## Stage 1 验证

- Artifact 领域构造只接受 succeeded、合法 UTF-8 JSON、<=1 MiB 的 Provider Observation。
- `pnpm check:architecture` 通过，新增领域类型和 Postgres 适配没有破坏 Clean Architecture。
- 隔离 PostgreSQL 套件通过新 migration、RLS、Reconciler grant 和真实 Provider success 收敛；自有容器正常清理。

## Stage 2：Artifact Query API

已新增 Run 下的 Artifact 列表和单 Artifact 读取：

- `GET /api/v1/workspaces/:workspace_id/runs/:run_id/artifacts`
- `GET /api/v1/workspaces/:workspace_id/runs/:run_id/artifacts/:artifact_id`

两条路径都复用当前 machine-key `run:read` 授权，授权先于任何 projection 访问。列表只返回 `artifact_id/kind/media_type/size_bytes/created_at`；单 Artifact 额外返回原生 JSON `content`。未知 query string 被拒绝，不默默接受未来含义；Run 或 Artifact 在当前 Workspace 不存在时返回 404。

查询应用层只依赖 `ReadRepository` 的 Artifact projection port；PostgreSQL adapter 只读取 `execution.artifacts`，不读取 `provider_observations`，因此公开结果查询不需要 Provider request/task/control 证据权限。应用层对 Workspace、Run、Artifact ID、类型、媒体类型、大小、时间和 JSON 再次进行 projection 校验，错误或跨租户数据 fail closed。

新增 `contracts/artifact-read.openapi.yaml` 作为已实现子集合同。HTTP / application 测试确认 run:cancel-only Key 无法读取 Artifact、撤销/缺失认证不能触达 projection、列表不含 content、detail 不含 provider request/task ID、credential/canonical arguments 等内部字段。

Stage 2 的 focused Go tests、`pnpm check:contracts` 和 `pnpm check:architecture` 已通过。runtime/query 数据库角色对 `execution.artifacts` 的最小 SELECT 仍留在 Stage 3 与真实 PostgreSQL 一起验证。

## Stage 3：集成与权限加固

Runtime reader 只获得 Artifact 的 `workspace_id/run_id/id/kind/media_type/content_json/created_at` 列级 SELECT；`source_observation_id` 明确不可读，Artifact 也不可 UPDATE/DELETE。Reconciler 为了 terminal success 原子物化，只新增 Artifact SELECT/INSERT，继续没有 UPDATE/DELETE。

隔离 PostgreSQL 验证覆盖：真实 Provider success 只产生一个 immutable Artifact；Provider replay 不重复物化；非 succeeded observation 不产生成功 Artifact；runtime RLS 隐藏其他 Workspace；受保护 HTTP list/detail 重新鉴权且不泄露 provider control 字段；并使用 integration-build-only migration boundary 创建 `0015` 历史成功结果，再应用完整迁移，验证 `0016` 对已有 succeeded result 的回填。

最终阶段门禁继续执行 `pnpm test:integration:docker`、`pnpm check`、`go test -race -count=1 ./...` 和 `git diff --check`。测试辅助的 partial-migration 入口仅在 `integration` build tag 下存在，生产 operator 仍只能执行完整 `migrations.Apply`。

## 本阶段明确不包含

不包含对象存储、签名 URL、文件上传、Artifact 删除策略、Webhook、Outbox external delivery、MCP Server/Client、Agent Adapter、真实支付、Provider cost 或公网部署。正式 WBS、ADR 和 G0–G5 状态不因本阶段代码自动推进。
