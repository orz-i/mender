# Mender：可运行纵向 Alpha 第二批

日期：2026-09-12。本记录只描述已落地代码和本地验证，不推进正式 WBS/G0–G5 状态。

## Mounted SecretProvider

Supply 新增 `adapters/outbound/filesecret`。它从**显式配置、operator/secret-manager 管理的挂载目录**读取 secret；进程环境只保存目录位置，永远不保存供应商 secret 值。文件名不是 `credential_version_ref` 本身，而是 Provider ID、Connection ID、Credential Version Ref、Connection Revision 四项绑定后的 SHA-256，因此 Rotation/Connection/Provider 变化都会映射到不同文件，用户输入也不能成为路径片段。

Provider 解析 root 和 projected-secret symlink 后，最终文件必须仍在 resolved root 内、是 regular file、大小 1–16 KiB。Kubernetes/CSI 一类“目录内 symlink 到版本数据”的投影可工作；指向 root 外的 symlink 会失败关闭。secret 仍只以 `supply/application.Secret` 存在内存中，String/GoString 固定 `[REDACTED]`。

运维侧可用 `filesecret.FileName(SecretRequest)` 的同一算法计算文件名。应用不会生成、打印或写入 secret；实际文件权限、挂载只读属性、底层 KMS/secret manager ACL 与轮换是部署责任。

## Reviewed worker host

`cmd/worker` 现在调用 `RunWorkerEntrypoint`，但**默认行为没有放宽**：`MENDER_REVIEWED_WORKER_RUNTIME_ENABLED` 默认为 false，此时仍走原 fail-closed worker，不构造 SecretProvider、供应商 egress 或 settlement principal。

显式启用后，host 可以组合三个独立 capability：

1. HTTP dispatch：要求原 `MENDER_WORKER_DISPATCH_ENABLED=true`、独立 worker/executor roles、mounted secret root 与精确 egress host allowlist；两个 dispatch flag 必须一致。
2. Provider control：要求独立 executor/reconciler roles、reviewed Provider IDs、mounted secret root 与同一 egress policy。Provider endpoint 仍只能来自 Supply Deployment，环境变量不能覆盖 URL。
3. Usage settlement：要求独立 settlement role，可在没有 supplier egress/secret 的情况下启用；仍依赖显式 worker Workspace scope。

`ReviewedWorkerServices` 每个 poll cycle 对每个 Workspace 至多执行一次 provider-control cycle 和一次 settlement claim。没有内部无限循环、自动租户发现、额外 goroutine 或自动放宽并发。

## 配置安全边界

- 所有 bool 只接受空/false/true；`yes` 等值拒绝。
- subordinate capability 不能在 master switch 关闭时静默生效。
- provider IDs 与 egress hosts 都必须是非空、无重复的显式列表；HTTP adapter 再次做 canonical host/SSRF/DNS pinning 检查。
- raw secret、Provider endpoint、Workspace 自动发现都不来自 ambient config。
- worker/executor/reconciler/settlement 角色仍由各自 startup checker 验证最小权限与 migration digest。

## 实际验证

真实隔离 PostgreSQL 套件已通过：Provider Control integration 改为由 `BuildReviewedWorkerServicesFromConfig` 组合 mounted SecretProvider，再走本地 HTTP status/cancel；服务端确认 Bearer secret 正确且 body 不包含 Workspace/canonical arguments/secret。Usage Settlement integration 的第一笔成功结算改由 reviewed worker host cycle 完成，其余 fault rollback、并发 single-owner 和 replay idempotency 仍由原测试覆盖。

本阶段仍**不是**云厂商 Secrets Manager/KMS SDK 实现，也不包含自动 secret provisioning、rotation controller、callback/webhook、支付或 supplier payout。upstream MCP runtime 仍是显式 library composition，尚未接入该环境宿主的 transport multiplexer。
