# Mender：Provider HTTP Control Runtime

日期：2026-09-11。本阶段在 durable Provider Result / Reconciliation / Cancellation 语义之上，为 HTTP Provider 增加受审的 status/cancel control plane。目标是让已经持久化的 provider request/task handle 可以通过 Supply-owned adapter 查询或取消，同时继续保持 Execution 与供应商协议隔离。

## Stage 1：不可变 control endpoint contract

`0013_provider_control_endpoints.sql` 给 `supply.deployments` 增加可选的 `status_endpoint_url/status_http_method` 与 `cancel_endpoint_url/cancel_http_method`。每组字段必须同时为空或配置为固定 `POST` endpoint；provider request ID、external task ID 和 cancel key 不会被插入 URL 路径或查询参数，后续 adapter 只能把它们放进有大小限制的 JSON body。

Supply `Deployment` domain 同步校验字段配对、scheme、host/userinfo/fragment 基础合法性，并暴露 `SupportsStatusQuery` / `SupportsCancellation`。这只是发布合同能力标记；真正的 SSRF、DNS pinning、allowlist、redirect/proxy 和 TLS 防线仍必须由 HTTP adapter 在网络前执行。

Executor runtime 继续是唯一可以读取 Supply deployment 与 opaque `credential_version_ref` 的数据平面角色；Reconciler 仍不能读取 Supply、Connections 或 admitted arguments。`0013` 不为任何现有角色增加写权限，也不启用公网请求。

本切片的 Supply domain / repository 编译与测试已通过。真实隔离 PostgreSQL 套件在当前宿主因 Docker daemon 未启动无法执行，脚本明确报告没有创建资源；该失败作为环境诊断记录，阶段最终仍保留 real PostgreSQL 强制 gate，不能据此宣称迁移已经在数据库验证。

## 尚未在 Stage 1 开放

尚无 HTTP ProviderStatusReader / ProviderCanceler、provider-control runtime、生产 SecretProvider、callback/webhook、Artifact、结算或 Outbox 外部投递。后续切片必须继续使用已持久的 provider identity/handle，不允许根据 request ID 猜路由或因 unknown outcome 盲重试。
