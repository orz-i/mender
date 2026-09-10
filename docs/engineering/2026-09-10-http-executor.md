# Mender：Hardened HTTP Executor

日期：2026-09-10。本切片在 Supply Runtime Broker 之上实现 HTTP transport，但**没有开启生产 Worker dispatch，也没有访问任何真实外部供应商**。所有 socket 集成测试只连接 `httptest` loopback 服务；非 loopback 场景使用合成 resolver/dialer，不产生真实网络请求。

## 安全边界

HTTP Executor 属于 Supply outbound adapter。跨上下文只公开最小 `supply/public.Executor` 合同：Workspace、Run、Attempt/generation、durable submission key 以及 accepted/unknown 结果。它不暴露 secret、canonical arguments、Connection reference 或 HTTP client 类型。

每次 Submit 先调用 Supply Broker，重新取得当前 Deployment、Connection 授权和内存 secret。随后执行以下 preflight：

- egress 默认 deny；构造 Executor 必须提供精确 host allowlist，不支持 wildcard；
- HTTPS 是默认协议，HTTP 只有显式 `AllowHTTP` 才可用，当前仅用于 loopback 测试；
- URL 禁止 userinfo、fragment、非法端口和非 ASCII host；
- host 解析后检查**全部**地址；private、link-local、multicast、unspecified、CGNAT、loopback 默认拒绝；测试策略可以单独允许 loopback；
- DNS 只解析一次，随后 Transport `DialContext` 固定拨号已验证 IP；TLS 仍以原 host 做服务端身份校验，从而避免解析后 DNS rebinding；
- 禁用环境代理、redirect、keepalive 和 HTTP/2 自动复用路径；每个提交请求使用新的 Transport；
- Deployment timeout 覆盖 DNS、dial、TLS、header 和 response read；
- request body 必须是 Broker 给出的 canonical arguments，并再次受 Deployment `max_request_bytes` 限制；
- `Content-Type`/`Accept` 固定为 JSON，provider idempotency header 固定写入 durable submission key；
- bearer 或 custom header secret 必须是无控制字符的可打印值，CR/LF 注入在拨号前拒绝；Host、Content-Length、Authorization、Content-Type 等保留头以及 auth/idempotency 同名配置在 Deployment validation 阶段失败；
- response body 使用 `max_response_bytes + 1` 有界读取，避免无界内存增长。

## 结果语义

只有 HTTP 2xx、`Content-Type: application/json`、响应未超限、JSON 顶层严格只包含 `provider_request_id` 和可选 `external_task_id`、无重复键、且 provider request ID 合法时，结果才是 `accepted`。

3xx（不跟随）、4xx/5xx、transport timeout、连接中断、超大响应、非 JSON、重复键、未知字段或无有效 provider ID 均保守映射为 `unknown`。这是有意的：Execution Dispatcher 在调用 Executor 前已经持久化 submission intent，因此任何不能证明供应商未接受的情况都进入既有 reconciliation，而不是自动 release/requeue/re-submit。当前实现没有“确定 rejected → 自动重试”的捷径。

调用方 context 被取消时，HTTP Executor 返回原 cancellation；durable intent 仍由 lease-expiry recovery 收敛到 reconciliation。错误对象不包含 request headers、body 或 secret。

## 验证

本地测试覆盖：

- loopback 成功请求的 POST/JSON body、Bearer secret、idempotency key 和 accepted provider IDs；
- redirect 的目标服务零调用；
- deployment timeout、超大响应、重复 JSON key 和未知字段均返回 unknown；
- private、loopback（未授权）、CGNAT 与 public+loopback mixed DNS 在拨号前拒绝；
- resolver 返回的公共地址被固定为实际 dial address，证明请求阶段不会再次按 hostname 解析；
- 空 allowlist 默认失败关闭；
- auth/idempotency header 冲突和 CRLF secret 在拨号前拒绝。

验证命令：

```sh
go test ./tests/supply ./internal/contexts/supply/... ./tests/architecture
go test -count=1 ./tests/supply -run ^TestHTTPExecutor
```

两项均 exit 0。第二项启用了本地网络权限，但测试代码只创建 loopback `httptest` listener；合成 DNS 测试的 dialer 不建立真实连接。

## 仍未开放

该 adapter 尚未装入 `cmd/worker`。生产 `MENDER_WORKER_DISPATCH_ENABLED=true` 仍失败关闭。下一切片会增加 lease/heartbeat/Dispatcher supervisor，并保持生产 runtime 必须同时具备显式 executor DB、egress policy 与 secret-provider 能力才能启动；本任务不提供真实 secret-store，也不执行真实外部供应商调用。
