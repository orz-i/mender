# Mender：Provider HTTP Control Runtime

日期：2026-09-11。本阶段在 durable Provider Result / Reconciliation / Cancellation 语义之上，为 HTTP Provider 增加受审的 status/cancel control plane。目标是让已经持久化的 provider request/task handle 可以通过 Supply-owned adapter 查询或取消，同时继续保持 Execution 与供应商协议隔离。

## Stage 1：不可变 control endpoint contract

`0013_provider_control_endpoints.sql` 给 `supply.deployments` 增加可选的 `status_endpoint_url/status_http_method` 与 `cancel_endpoint_url/cancel_http_method`。每组字段必须同时为空或配置为固定 `POST` endpoint；provider request ID、external task ID 和 cancel key 不会被插入 URL 路径或查询参数，后续 adapter 只能把它们放进有大小限制的 JSON body。

Supply `Deployment` domain 同步校验字段配对、scheme、host/userinfo/fragment 基础合法性，并暴露 `SupportsStatusQuery` / `SupportsCancellation`。这只是发布合同能力标记；真正的 SSRF、DNS pinning、allowlist、redirect/proxy 和 TLS 防线仍必须由 HTTP adapter 在网络前执行。

Executor runtime 继续是唯一可以读取 Supply deployment 与 opaque `credential_version_ref` 的数据平面角色；Reconciler 仍不能读取 Supply、Connections 或 admitted arguments。`0013` 不为任何现有角色增加写权限，也不启用公网请求。

本切片的 Supply domain / repository 编译与测试已通过。真实隔离 PostgreSQL 套件在当前宿主因 Docker daemon 未启动无法执行，脚本明确报告没有创建资源；该失败作为环境诊断记录，阶段最终仍保留 real PostgreSQL 强制 gate，不能据此宣称迁移已经在数据库验证。

## 尚未在 Stage 1 开放

尚无 HTTP ProviderStatusReader / ProviderCanceler、provider-control runtime、生产 SecretProvider、callback/webhook、Artifact、结算或 Outbox 外部投递。后续切片必须继续使用已持久的 provider identity/handle，不允许根据 request ID 猜路由或因 unknown outcome 盲重试。

## Stage 2：Hardened HTTP status / cancel adapter

Supply HTTP Executor 现在同时实现 `ProviderStatusReader` 与 `ProviderCanceler`，但依旧只通过 Supply public contract 暴露给 Execution。Execution outbound adapter 将已经持久化的 Workspace/Run/Attempt/provider identity/request/task handle 传给 Supply；HTTP adapter 再用 Workspace/Run 去原 Admission 事实解析不可变 deployment、当前 Connection 授权与 opaque credential reference。Provider control 不获得 canonical arguments，也不把 Workspace、Run 或 secret 写进 provider 请求体。

`Broker.PrepareControl` 与 submission 复用同一 credential/deployment 解析边界，但允许已经 disabled 的 deployment 查询／取消此前提交的远端任务；禁用新 submission 与清理既有远端工作的语义因此分离。实际网络仍受独立 egress allowlist 控制。Submission 原有的 request-size-before-secret 不变量保持不变。

status 与 cancel 都只向 deployment 中预先审核的固定 `POST` URL 发送 JSON。request/task handles 只在 body 中出现。cancel 额外把 durable `cancel_key` 同时放在 JSON body 与 deployment 的 idempotency header 中；status 不发送新的幂等键。两条路径复用 Submit 的 host allowlist、DNS resolve + pinned dial、private/loopback/CGNAT 拒绝、redirect/proxy 禁用、TLS、timeout 与 response-size 控制。

status 成功响应采用严格字段／重复键校验，只接受 `pending/succeeded/failed/canceled` 与各状态允许的 result/error 组合；非法 JSON、非 2xx、timeout 或 transport error 都映射为 status unavailable，因此 Execution 不会写伪 observation。cancel acknowledgement 同样严格验证 observation ID/time；但 cancel 请求一旦越过网络边界，timeout、HTTP error、超大/错误 content-type 或 malformed success 都映射为 `CancelUnknown`，配合已持久的 `sending` intent 保证后续不会自动重发取消请求。

本切片新增受控 loopback `httptest.Server` 测试，验证固定 status/cancel path、bearer auth、durable cancel idempotency key、请求体最小化、严格 Provider result、timeout/malformed→unknown、redirect 不跟随、provider identity mismatch fail-closed，以及 disabled deployment 仍可 reconciliation。没有访问真实供应商。

验证：`go test -count=1 ./tests/supply ./tests/execution ./internal/contexts/supply/... ./internal/contexts/execution/adapters/outbound/supplystatus ./internal/contexts/execution/adapters/outbound/supplycancel` 通过；`pnpm check:architecture` 通过。架构门禁曾发现 Supply domain 引入 `net/url`，已把完整 URL/SSRF 解析职责退回 HTTP adapter，domain 仅保留纯字符串结构约束，随后门禁复验通过。

## Stage 2 之后仍未开放

目前 adapter 已可在显式构造时调用受控 HTTP Provider，但默认 `cmd/worker` 没有生产 SecretProvider 或 Provider Control runtime，仍不会发起 status/cancel 网络请求。下一切片只负责 reviewed runtime composition 与 fail-closed 运行边界，不新增 callback/webhook、Artifact、结算、Outbox 外部投递或真实 provider 配置。

## Stage 3：Reviewed Provider Control Runtime

bootstrap 新增 `BuildProviderHTTPControlRuntime`，只接受显式传入的 executor DB URL、reconciler DB URL、审核 Provider ID 列表、egress allowlist 与 `SecretProvider`。两个数据库连接必须指向同一个 PostgreSQL database、拥有不同用户名，并且所有 fallback host/port 完全一致。启动后分别执行 migration digest、`ExecutorRole` 与 `ReconcilerRole` 自检；因此 Execution control plane 仍看不到 Supply/Connections/arguments，Supply control plane 则没有 Run/result/cancel intent 写权限。

executor role 负责读取 immutable admission/deployment 与当前 Connection credential reference，并在 Supply Broker 内解析 Secret；reconciler role 负责选择 provider target、claim durable cancel intent、追加 ProviderObservation 和收敛 Run/Job。两者只在 bootstrap 组合，Execution application 不 import Supply，Supply adapter 不持有 reconciler repository。

`ReviewedProviderControlRuntime.ControlOne` 是刻意有界的单步入口：每次最多 claim/调用一个 provider cancellation，再查询一个 provider status。没有取消任务时直接进入 reconciliation；取消 outcome unknown 时不会重发取消，而是继续允许一次只读 status query 尝试收敛；存储、权限或其他错误立即 fail closed。runtime 不创建后台 goroutine、无限轮询或新的 submission/Attempt，调用频率和 Workspace 选择必须由未来受审 host 显式决定。

默认 `cmd/worker` 与 API **没有**从环境变量构造 Provider Control Runtime，也没有生产 SecretProvider；因此本阶段新增的 runtime library 不会让默认进程突然访问供应商。`backend/.env.example` 明确保留这一边界。生产 host／SecretProvider、Provider-specific response mapping、callback/webhook 仍是后续独立评审项。

新增 integration fixture 已编译，并设计为在现有 isolated PostgreSQL 套件中创建独立 worker/executor/reconciler/cancellation 角色，配合 `httptest.Server` 验证真实 RLS/ACL、Broker secret boundary、status→success 与 cancel→fulfilled 的端到端收敛。HTTP 只绑定 loopback；请求体断言不含 Workspace、Run canonical arguments 或 secret。当前宿主 Docker daemon 在 Stage 1 试跑时不可用，因此这项 real PostgreSQL fixture 只有在最终 `provider-control-phase-postgres` gate 实际成功后才能记为通过。

## 本阶段完成后仍不包含

不包含公网 Provider 配置或真实供应商调用、生产 SecretProvider、provider callback/webhook、Artifact/object storage、Commerce settlement/ledger、Outbox external delivery、生产调度命令、部署与 push。正式 WBS/ADR/G0–G5 也不因本阶段代码和本地测试自动推进。
