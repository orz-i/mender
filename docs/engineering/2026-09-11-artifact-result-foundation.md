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

下一切片将新增 Run 下的 Artifact 列表与单 Artifact 读取。HTTP 层会复用当前 machine-key `run:read` 授权和 Workspace 隔离；公开响应只包含稳定 Artifact 元数据与 JSON content，不暴露 provider request/task ID、Provider control 状态、Connection secret 或 canonical arguments。

## Stage 3：集成与权限加固

最后一切片补充 runtime reader 的最小 Artifact SELECT、跨租户、历史回填、最大内容、幂等和 API 集成测试，再执行 `pnpm test:integration:docker`、`pnpm check`、`go test -race -count=1 ./...` 和 `git diff --check`。

## 本阶段明确不包含

不包含对象存储、签名 URL、文件上传、Artifact 删除策略、Webhook、Outbox external delivery、MCP Server/Client、Agent Adapter、真实支付、Provider cost 或公网部署。正式 WBS、ADR 和 G0–G5 状态不因本阶段代码自动推进。
