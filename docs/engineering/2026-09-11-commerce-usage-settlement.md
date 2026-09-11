# Mender：Commerce Usage Settlement

日期：2026-09-11。本阶段接在已验证的 Provider HTTP Control Runtime 之后，优先补齐 S2 的“执行终态 → 预算结算”闭环，再进入 MCP／Agent、Artifact 或 Webhook。

## Stage 1：固定 success-only 价格与配额结算合同

`0014_usage_settlement_contract.sql` 将既有 `PriceVersion.reserve_micro` 拆出明确的 `charge_micro` 与 `billing_policy=fixed_success_only`。迁移对既有价格版本按 `charge_micro=reserve_micro` 回填，保持已发布固定价格语义；新合同要求 `0 <= charge_micro <= reserve_micro`。

首版结算政策刻意有限：Provider `succeeded` 才消费 `charge_micro`；Provider `failed`／`canceled` 的实际 charge 为 0，并释放整笔 held reservation。`running`、`reconciling`、unknown outcome 不是可结算结果，后续流程不得因为本地 timeout 自动释放额度。

Commerce `Allowance.Settle(reserved,charged)` 只把已持有 quota 从 `reserved` 转成 `consumed`，并检查 charge 不超过 reservation。它仍明确 **不是** payment wallet、revenue ledger 或复式会计总账。

Reservation 新增 `settled` 状态、`charged_micro` 与 `settled_at`。`commerce.usage_settlements` 是不可变的 Run 级使用结算事实；Reservation 与 usage settlement 通过 deferred 双向 proof 约束，避免只更新预算或只写收据的部分提交。现有 coordinated cancellation 的 `released` 状态保持不变。

PriceVersion、Budget、Reservation 与 UsageSettlement 继续归 Commerce 上下文所有。Execution terminal fact 和跨上下文 settlement job／UoW 会在下一切片实现；本切片没有让 Provider result handler 直接写 Commerce。

## 已验证

- `go test -count=1 ./internal/contexts/commerce/... ./tests/admission`：通过。
- `pnpm check:architecture`：通过。
- `pnpm test:integration:docker`：迁移 0014 在真实隔离 PostgreSQL 上应用并复验既有 migration/RLS/CAS/query/admission/provider-control 套件通过，自有容器已删除。

## 尚未实现

尚未把 terminal Provider result 转成 settlement job，也未实现 settlement principal、跨上下文 UoW、失败注入或 settlement API/read model。支付充值、收入／成本分录、退款、Provider cost、Artifact、Webhook、Outbox external delivery、MCP／Agent 仍不在本阶段范围。

## Stage 2：Durable Settlement Job 与受控跨上下文 UoW

`0015_terminal_settlement_jobs.sql` 新增 Execution-owned `execution.settlement_jobs`。Provider `succeeded/failed/canceled` 终态 Observation 被接受时，reconciler 在**同一 Execution 事务**插入 pending settlement job；Provider cancellation acknowledgement 走同一路径。数据库 deferred trigger 在提交时再次证明 Observation、Run 与 Job 已形成一致终态。这样结算触发不依赖进程内回调，Worker/Reconciler 崩溃后仍可恢复。

Reconciler 只获得 settlement job 的 INSERT 权限，不能读取、更新或删除已经创建的 job。新的 settlement principal 才能读取 pending job 并仅更新 `state/finished_at`；它读取 `run_admissions` 时只获得 reservation/price/budget/currency 等结算引用，不可读取 `canonical_arguments`、subject、credential、Connection、Toolset 或 deployment。该角色没有 Identity、Connections、Supply、Catalog、Distribution schema 使用权。

`internal/processes/settlement` 是明确的 ADR-020 同库协调流程，而不是新的业务上下文。application 只定义 `Claim → Settle → Finish` 端口；PostgreSQL UoW 打开一个事务并设置 Workspace RLS，outbound capability adapter 只依赖 `commerce/public.Settler`。Commerce 自己锁 Budget/Reservation、验证 immutable PriceVersion、把 held reservation 原子转为 consumed quota，并写 append-only UsageSettlement；Execution 自己把 settlement job 标记 finished。SQL transaction 不泄露到 application/domain。

`ReviewedUsageSettlementRuntime` 仍是一个无后台循环的受审 library composition。默认 API/Worker 不自动运行它；后续 host 才能决定 Workspace 和 cadence。结算事务内没有 Provider HTTP、SecretProvider、Webhook 或支付网络调用。

Stage 2 单元测试覆盖成功 charge、failed/canceled 零 charge、未知 outcome、immutable replay、未来/过早 Observation、无任务与失败不误标 finished；架构门禁新增 `process:settlement` 规则，禁止 application 直接依赖 Commerce public/pgx，且跨域只能由 outbound adapter 通过 public contract。
