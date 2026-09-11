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
