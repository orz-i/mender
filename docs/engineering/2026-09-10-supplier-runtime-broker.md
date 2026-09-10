# Mender：Supplier Runtime Input / Credential Broker

日期：2026-09-10。本阶段在已提交的 Executor Dispatcher 与 durable submission protocol 基础上增加供应商执行材料边界，重点是**不扩大 Worker lease role，不把 secret 存进 Mender 数据库，也不把 StartRun 时的 Connection 授权永久视为有效**。

## DDD 所有权与调用链

Execution 仍拥有 `run_admissions`，新增只读 RuntimeInput service/public facade，仅发布已受理且不可变的 Workspace、Run、Subject、Connection、ToolVersion、Deployment revision 和 canonical arguments。Connections 新增 RuntimeCredential service/public facade，执行时重新检查 Connection state、Grant、provider、revision 和有效期，只发布 opaque `credential_version_ref`；它不发布 secret。

Supply 新增 `supply.deployments` 与 Broker。Broker 通过 outbound ACL 调用 Execution/Connections public port，再读取 Supply 自有 Deployment。对于需要认证的 Deployment，最后一步才调用外部 `SecretProvider`，参数只有 provider、connection、connection revision 和 opaque credential version reference。返回的 `Secret` 只保存在内存，普通格式和 Go `%#v` 均固定显示 `[REDACTED]`；没有任何数据库字段、RunEvent、Attempt 或日志接收该值。

执行时序为：`RuntimeInput → Deployment → current RuntimeCredential → SecretProvider`。Connection 在排队期间被 revoked/expired、Grant 被撤销、provider 不匹配、Deployment disabled 或请求体超过 Deployment 上限时都会在 secret 访问前失败关闭。`auth_mode=none` 时不会调用 SecretProvider。

## PostgreSQL 与角色

新增前向迁移 `0009_supplier_runtime.sql`，只创建 Supply 自有的非秘密 HTTP Deployment 描述：provider、endpoint、POST、auth mode/header、provider idempotency header、timeout、request/response 上限和状态。表中不允许 credential secret。

操作员创建独立无特权 login role 后执行：

```sh
pnpm db:migrate
pnpm db:grant-executor --role mender_executor
```

Executor role 只获得 `execution`、`connections`、`supply`、`mender_meta` 必要 USAGE 和列级 SELECT。Execution/Connections 仍使用 FORCE RLS；repository 每次短事务用 `mender.workspace_id` 限定租户。该角色不能读取 Identity API keys、Commerce budget/price/reservation、Catalog、Job/Attempt/Run/Event/Outbox/Cancellation，也不能写任何 invocation source table。Worker role 继续没有 Supply schema USAGE，也不能读取 canonical arguments 或 `credential_version_ref`。

`database.ExecutorRole` 启动自检按“schema 可达性 + 可达 schema 内表/列权限”判断有效权限，避免把没有 schema USAGE 的底层 ACL 错报为实际数据访问。API/admission/cancellation/worker 自检同时将新 Supply schema 纳入受保护范围。

## 验证

单元测试覆盖：当前 credential authority facts 被传给重新校验、secret provider 只收到 opaque versioned reference、secret 格式不可泄漏、无认证不取 secret、provider mismatch / revoke / 请求超限在 secret 前失败。

真实隔离 PostgreSQL 套件覆盖 `0009`、operator `grant-executor`、ExecutorRole、Execution RuntimeInput RLS、Connections RuntimeCredential RLS、Supply Deployment、成功 Broker 组合、Connection revoke 后 secret provider 不再被调用，以及 executor/worker/runtime 等角色互不替代。第一次真实库运行暴露 privilege-helper 会报告“底层 ACL 但 schema 不可达”的误判；修正为有效权限模型后同一验证键重跑通过并 supersede 失败，测试容器已清理。

本阶段没有真实供应商网络调用，也没有 secret-store 实现。下一切片在此 Broker 输出上实现 hardened HTTP transport，并只使用本地受控 HTTP server 验证 SSRF/redirect/size/timeout/idempotency 行为；生产外部 egress 继续关闭。
