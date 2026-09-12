# 2026-09-12 Usage / Budget Observability Alpha

本切片为 Human Console 增加 Commerce quota 的只读可解释投影。它回答“当前额度窗口还有多少、哪些 Run 仍占用预留、哪些 Run 已经结算/释放”，不实现充值、支付、收入、退款、发票或复式账本。

## 服务端边界

- `GET /api/console/v1/workspaces/{workspace_id}/usage` 只接受 HttpOnly Browser Session；没有 query 参数、没有匿名入口。
- Identity 每次重新校验当前 Workspace membership 的 `usage:read`。Owner/Admin/Developer/Viewer 均可读，但这不会授予任何 billing/admin 写权限。
- Commerce 使用单独的 `commerce-observer` PostgreSQL role，并在事务内设置 `mender.workspace_id`，继续依赖 RLS 防止跨 Workspace 读取。
- 该角色只读 `budget_periods` 的额度列、`reservations` 的安全 quota 状态/金额/时间列，以及 `usage_settlements` 的 Workspace/Run/outcome；不能读取 settlement reservation/price 内部引用，也不能访问 Identity/Execution/Connections/Supply。
- 返回最多 100 个 Budget period 和 100 条最近 reservation projection；异常或破坏不变量的数据 fail-closed。

## 金额与状态语义

所有额度继续以 `bigint` micro unit 持久化，并以规范十进制字符串返回 HTTP；前端无需也不允许用浮点数作为权威金额表示。

Budget period 暴露：`limit_micro`、`consumed_micro`、`reserved_micro`、`available_micro = limit - consumed - reserved`。Usage entry 暴露 `held|released|settled`、原 reservation、实际 charged、已释放额度及 terminal outcome。`released_micro` 是 quota 释放量，不是支付退款。

`fixed_success_only` 仍保持既有语义：成功 settlement 可把固定 charge 转为 consumed quota；失败/取消 charge 为零并释放 reservation。该数据是 Commerce quota 事实，不是 payment/revenue accounting。

## 配置

默认关闭：

```text
MENDER_CONSOLE_USAGE_ENABLED=false
MENDER_COMMERCE_OBSERVER_DATABASE_URL=postgres://...
```

管理员只给预先存在的非特权角色授权：

```text
pnpm db:grant-commerce-observer --role mender_commerce_observer
```

API 启动时验证它与 runtime/browser-session 是同一个 PostgreSQL database 的不同受限 principal；grant 不匹配时 fail-closed。

## 当前不声明

- payment wallet / top-up / refund / revenue ledger / invoice；
- Budget 创建、调额或 billing administration；
- 无限 Usage 历史、导出或聚合分析；
- Provider 成本、平台毛利或财务对账。

这些仍属于后续 Commerce/Product 阶段，不能从本 Alpha 的 quota 可视化推断已经实现。
