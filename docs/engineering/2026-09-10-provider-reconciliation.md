# Mender：Provider Status Reconciliation

日期：2026-09-10。本切片在 `0010` Provider Result 持久协议之上增加异步状态查询编排。**验证使用真实隔离 PostgreSQL + 内存 fake provider reader；仓库没有 production provider polling 网络 adapter，也没有执行真实外部请求。**

## Durable provider routing

`0011_provider_reconciliation.sql` 给 `execution.run_attempts` 增加 `provider_id`。所有新的 `submitted` transition 必须同时持久 `provider_id + provider_request_id`；若 supplier 已返回 accepted 后本地 accepted bundle 无法持久，但数据库仍可写，unknown fallback 也可保存已经确认得到的 provider/request/task handles。只有完全未知的 transport outcome 才保持这些 handles 为空。

迁移允许 pre-0011 的历史 submitted/unknown row 保留 `provider_id=NULL`，以避免前向迁移破坏既有数据；但 Provider Reconciler 的 candidate query明确要求 provider_id/request_id 都存在，因此历史缺路由事实的任务不会被自动发送给任何 provider。系统宁可保留人工 reconciliation，也不根据 request ID 形状或当前 Supply 配置猜路由。

`provider_observations` 同样新增 nullable-for-legacy `provider_id`，但所有**新 INSERT**由 trigger 强制非空；deferred provider-result bundle 同时匹配 Attempt 的 provider_id 与 provider_request_id。这样数据库层也能防止两个供应商恰好使用相同 request ID 时串线。

## Reconciler application

Execution application 新增 `ProviderReconciler`：

1. Reconciler repository 在一个短 RLS transaction 中选择 `Job=provider_waiting/reconciling`、`Run=running/reconciling`、Attempt=`submitted/unknown` 且具有完整 provider identity 的一个候选，然后提交事务。
2. 数据库事务结束后才调用 `ProviderStatusSource`，因此没有行锁跨越 provider I/O。
3. Execution outbound `supplystatus` adapter 使用 provider_id 精确选择 Supply `ProviderStatusReader`；未知 provider 没有 fallback。
4. provider 返回的 observation ID/state/result/error/time 先组成 domain `ProviderObservation` 并严格验证，再交给 Stage 1 `ProviderResults` append/terminal bundle。
5. provider status unavailable、非法 observation 或 caller cancellation 都不会创建 observation、改变 Run/Job，也不会重新 Submit、Release 或创建新 Attempt。

并发 poll 允许重复发生：candidate 选择不持有跨网络锁，最终 durability 依赖 `(workspace,run,observation_id)` 幂等、observation 时间单调约束、Run→Job 行锁与 terminal bundle 串行化。该模型保证数据库收敛，不声称 provider 查询本身 exactly-once。

## Supply contract

Supply public 发布 `ProviderStatusReader`，输入仅为 provider identity/request/task handles，输出稳定 observation ID、`pending/succeeded/failed/canceled`、可选 result JSON/error code 和 observed_at。Execution application 不 import Supply；跨上下文依赖只存在于 execution outbound adapter。

当前没有 HTTP status adapter。不同供应商对 polling endpoint、签名、ETag、callback、长轮询等能力差异留给后续 provider-specific adapter/capability 设计；本切片只固定不会随具体 SDK 变化的应用合同。

## 验证

单元测试覆盖：无候选不调用 provider；invalid target/status 不落库；provider unavailable 不落库；pending/succeeded/failed 映射；unknown provider identity 不 fallback。真实隔离 PostgreSQL + fake reader 验证：accepted Attempt 持久 provider_id；candidate 按该 identity 路由；pending 保持 running/provider_waiting；下一 observation success 原子收敛为 succeeded/finished；终态后候选消失；两次 polling 后 Attempt 数量仍为 1；provider status unavailable 时 Run version、Job、Attempt 与 observation 数量全部不变。`0010` 的 replay/乱序/terminal bundle 测试同时继续通过。

下一切片处理 provider cancellation intent/outcome 与“取消确认 vs 迟到成功/失败”的 terminal convergence；仍只使用 fake/local provider，并保持默认 production wiring fail-closed。
