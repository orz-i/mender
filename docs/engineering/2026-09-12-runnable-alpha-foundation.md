# Mender：Runnable Vertical Alpha Foundation

日期：2026-09-12。本阶段承接已经验证的原子 Admission、Worker 租约、HTTP／MCP Supply、Provider Control、Artifact 与 Usage Settlement，目标不是继续增加协议类型，而是消除入口合同差异，并建立一个可由受信宿主显式装配的有界运行循环。

## Slice 1：所有 StartRun 入口共享业务参数 Schema 校验

此前 Fixed Toolset MCP 在协议入站层校验 ToolVersion `input_schema`，REST StartRun 与 `mender_run_start` 则只通过 Admission 解析 Toolset、Connection、Price 与 Budget。这样同一 ToolVersion 的非法业务参数可能在不同入口得到不同处理。

现在校验统一位于 `processes/admission/adapters/outbound/capabilities.Resolver`：先解析 immutable ToolVersion，再用其已发布 `input_schema` 校验 canonical business arguments，然后才访问 Connection／Pricing，最后才进入预算预留与 Run/Job/Outbox 原子事务。Admission application/domain 不依赖 JSON Schema 库；Fixed Toolset MCP 只保留协议控制块解析，不再维护第二套业务规则。

兼容边界：历史 ToolVersion 的空 `input_schema` 暂时不被强制拒绝；一旦 Schema 存在就必须通过共享校验。外部 `$ref` 没有 Loader，因此 fail closed 且不会因 Schema 解析产生网络访问。参数 canonical bytes 不因校验被改写，`9007199254740993` 等整数仍按原始精度进入幂等摘要和执行输入。

真实 PostgreSQL 套件新增两条负向证明：REST StartRun 和 Fixed Toolset MCP 都会在创建 `run_admissions`／持有 quota 之前拒绝不符合已发布 Schema 的参数。

提交：`dee950d1711064d1119ad16d276d6ae9f9a3f2fb` (`fix(admission): 统一工具参数 Schema 校验`)。

## Slice 2：受审 Worker Runtime Host 基础

已有组件此前都是安全但分散的 library composition：Worker dispatch supervisor、Provider HTTP Control runtime、Provider reconciliation 和 Usage Settlement 都可以在测试／受信调用方中工作，但默认 `cmd/worker` 没有生产 SecretProvider，也没有统一调度 Provider Control 与 Settlement。

新增 `ReviewedWorkerServices` 作为**显式能力集合**，可装入经过审核的 dispatch、provider-control 和 settlement runtime。新增 `RunReviewedRuntimeCycle`：对调用方明确提供的 Workspace 列表，每轮、每 Workspace 最多执行一次 Provider Control 和一次 Usage Settlement claim；它不自行选择租户、不构造 SecretProvider、不创建 goroutine、不无限轮询，也不改变数据库角色。

现有 Worker poll loop 在 lease recovery／可选 dispatch 后调用该有界 cycle。`RunWorkerWithReviewedServices` 可供未来受信 host 传入已审核能力；已有 `RunWorkerWithReviewedRuntime` 保持为仅 dispatch 的兼容入口。默认 `RunWorker` 仍传入 nil capabilities，因此**不会**因本阶段改动自动访问供应商或运行结算。若只配置 background runtime 却不显式开启 Worker control／Workspace scope，会在数据库访问前 fail closed。

Usage Settlement runtime 的内部 service 改为最小 `SettleOne` runner interface，仅用于外层装配和测试，不改变 settlement application/domain。Provider Control 的真实 PostgreSQL＋loopback HTTP 集成已改由新的 runtime host cycle 调用，继续验证独立 executor/reconciler 角色、Broker secret 边界、status/cancel 单次收敛。

提交：`fc23e7adb9e5fa592121a83ccfc8b2f437a3ee0b` (`feat(worker): 建立受审运行宿主调度基础`)。

## 验证证据

- `go test -count=1 ./tests/admission ./tests/mcp`：通过。
- `go test -count=1 ./internal/bootstrap`：通过。
- `pnpm check:architecture`：通过；JSON Schema 实现仍在 adapter，application/domain 未新增具体依赖。
- `pnpm test:integration:docker`：两次切片验证均通过真实隔离 PostgreSQL；最终 Slice 2 运行 8.674s，并清理自有容器。
- 最终 `pnpm check`：通过，包含固定工具链、workspace、122 个本地 Markdown 链接、合同、Go/TypeScript 架构门禁、lint、typecheck、20 项 Node 测试、Go test/vet/build 与 Console/Admin production build。
- 最终 `node scripts/backend.mjs test -race -count=1 ./...`：通过，禁用测试缓存重新运行全部非 integration Go 包并启用 race detector。
- 最终 `pnpm test:integration:docker`：通过，真实隔离 PostgreSQL integration suite 8.178s，自有容器清理成功。
- 最终 `git diff --check`：通过。

## 当前仍然没有开放

本阶段没有新增生产 SecretProvider、真实供应商凭据配置、自动从环境变量构造 egress allowlist，也没有把默认 Worker 改成自动对公网执行。Provider-specific 响应映射、Webhook、对象存储、支付／复式账本、人类 OIDC、成员后台、完整 Catalog/Connection 管理和 Agent/A2A 仍是后续范围。

前端仍主要是 Console／Admin shell 与 `/status`；本阶段没有把后端能力包装成完整 Run Explorer。WBS、ADR 与 G0–G5 状态不因这些代码提交自动标记完成。

