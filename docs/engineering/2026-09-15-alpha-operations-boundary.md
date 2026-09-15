# Core Alpha operational boundary｜S3-16 / S3-17

日期：2026-09-15  
机器证据：[alpha-operations-evidence.json](alpha-operations-evidence.json)

## 收口结论

S3-16 / S3-17 在 **Core Alpha** 范围内按“安全边界已验证、生产设施延后认证”收口。这里的 `covered-alpha` 不表示生产反向代理、SSE、告警平台或云对象存储已经上线；它只表示仓库内当前可运行能力的网络、超时、保留、删除和可审计边界已经有阻断式自动化证据。

## S3-16：传输、超时与网络策略

当前认证的 MCP surface 是 `stateless-streamable-http-json-response`。Meta-tool 与 Fixed Toolset 都由 SDK Streamable HTTP handler 处理，启用 request cancellation propagation；当前 Gate **不认证 standalone SSE**，也不认证由 Nginx、Envoy、Ingress、LB 或公网网关承载的 production streaming proxy。

应用侧 Run Event 仍然是 **finite JSON pagination**，首屏固定 through-version，后续页沿同一 watermark 读取。它不是 SSE，也不是 Outbox consumer；因此应用事件分页的 cursor 不能被描述成 MCP streaming resume token。

供应商出站只允许 Deployment 中固定 endpoint，并要求 operator 提供 exact DNS host allowlist。`MENDER_REVIEWED_EGRESS_ALLOW_HTTP=false` 与 `MENDER_REVIEWED_EGRESS_ALLOW_LOOPBACK=false` 是默认值；没有显式 opt-in 时不开放明文 HTTP 或 loopback。Deployment 的 `request_timeout` 必须位于 **100 ms–5 min**，请求／响应还有独立 size 上限。Alpha 不通过移除 SSRF/allowlist/timeout 检查来扩大兼容范围。

因此 S3-16 的 Alpha 决议是：网络与超时安全合同通过；production streaming proxy、standalone SSE 和 application SSE 仍为 deferred / not certified，进入后续生产化与 SRE Gate，而不是被伪装成已经完成的基础设施。

## S3-17：Artifact retention 与 callback 可审计性

Artifact Object materializer 默认 retention 为 **24h**，允许配置范围是 **1h–2160h（90 天）**。对象过期流程必须先执行 **physical delete**，删除成功后才允许数据库从 `available` 单向进入 `expired`；删除失败时不能把 metadata 标记成已删除。短时 capability 仍受对象 TTL 约束，过期对象不再签发新 capability。

Provider Callback 使用 durable Inbox 保存已验签后的安全 delivery metadata、disposition、reason 与 delivery count。Admin callback observer 只能读取安全投影，并且不能读取 raw body、signature、secret、provider request handle 或外部 task handle。重复、冲突、乱序和 terminal replay 都会留下可审计 disposition/quarantine 事实。

当前 **没有认证外部 callback alert transport**：没有把 PagerDuty、Slack、email、Webhook fan-out 或消息总线写成已完成能力。Alpha 的故障可见性是 durable Inbox + Admin safe projection；外部主动告警属于后续 SRE/生产化范围。

## 明确未认证

- production streaming reverse proxy / ingress buffering policy；
- standalone MCP SSE；
- application RunEvent SSE；
- 外部 callback paging / alert transport；
- S3 / MinIO / cloud object storage 运维与 SLA。

这些限制不会阻止 Core Alpha 的内部集成 Gate，但会阻止把当前版本表述为 production-ready 或第三方公网 interoperability 已完成。
