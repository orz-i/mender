# 2026-09-12 Run Quota Cost Alpha

本切片把 Commerce 已持久化的 reservation/release/settlement 事实映射为单个 Run 的 quota cost，使 Run Explorer 能解释“预留了多少、实际消费多少、释放多少”，但不把 quota accounting 描述成支付账务。

## API 与授权

当 `MENDER_CONSOLE_USAGE_ENABLED=true` 且 Human Run delegation 已启用时，注册：

```text
GET /api/console/v1/workspaces/{workspace_id}/runs/{run_id}/cost
```

该路径只接受显式短期 Run delegation Bearer token，并重新验证 `run:read` 与 Workspace；Browser Session Cookie 不会授权该请求。Machine `/api/v1` 本阶段不声明 cost route，避免共享 client 暗示不存在的能力。

Commerce 仍拥有 reservation/settlement 表和查询实现。Execution 不直接 SQL 查询 Commerce 写表；bootstrap 只把 Identity 的公开 RunDelegations contract、Commerce application 与 `commerce-observer` adapter 显式组合。

## 返回事实

单 Run 投影只返回：

- `run_id`、`budget_id`、`period_id`、`currency`；
- `quota_state = held | released | settled`；
- `reserved_micro`、`charged_micro`、`released_micro`；
- terminal `outcome`、创建/完成时间；
- 固定 `accounting_scope = quota_only`。

所有 micro amount 都是规范十进制字符串。前端可以用 `BigInt` 验证加减不变量，但必须保留字符串作为交换表示，不得转为浮点权威值。

客户端 fail-closed 检查 `released <= reserved`；settled 时要求 `released = reserved - charged`，并拒绝未知字段，因此 `reservation_id`、`price_version_id`、settlement job、Provider control 或 Secret 字段不能静默进入 UI 模型。

## 持久化语义

- `held`：仍占用 reservation，`charged=null`、`released=0`；
- `released`：reservation 已释放，`released=reserved`；
- `settled`：使用 Commerce immutable settlement fact，`released=reserved-charged`。

真实 PostgreSQL 测试同时覆盖 Human StartRun 后的 held 记录，以及 Usage Settlement 后成功 charge/部分释放、失败 zero-charge/全部释放。

`charged_micro` 仍是 Mender quota consumed amount，不是信用卡扣款、供应商付款、收入或财务分录。
