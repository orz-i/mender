# 2026-09-12 Console Usage / Budget Observability Alpha

本阶段把 Commerce 的 quota 事实接入 Human Console，但不改变计费权威或支付边界。

## 已实现

- `/usage` 使用当前 HttpOnly Human Browser Session 获取 Workspace 列表，并读取服务端过滤后的 Budget / Usage 快照。
- 金额／额度全部以 canonical micro string 在 HTTP 与 TypeScript 之间传递，前端使用 `BigInt` 做格式化，不经过 `number` 浮点转换。
- Budget 展示 `limit / consumed / reserved / available`、period window 与 revision。
- Usage 展示 Run 对应的 `held / released / settled` quota 状态，以及 reserved、charged、released 和 outcome。
- Run Explorer 在既有短期 `run:read` delegation 下读取单 Run quota-cost 投影，展示 reservation、charge/release 与 settlement 状态。
- Console 不获得 Budget mutation、billing admin、充值、退款或支付权限。

## 安全边界

- `/usage` 的 Browser Session 只用于只读 `usage:read`，服务端每次重新检查当前 Workspace Membership。
- Commerce observer 使用独立最小 PostgreSQL role，并受 Workspace RLS 约束。
- 单 Run cost 使用短期 Run delegation；OIDC Cookie 不能直接替代 `run:read` capability。
- API 不返回 settlement job、Provider request、credential ref、secret、数据库控制字段。
- 当前 settlement 仍是 quota settlement，不是支付、收入确认、发票或复式账本。

## 验证

Focused API-client tests 覆盖 Browser Session transport、超出 JavaScript safe integer 的 micro string、内部字段泄漏 fail-closed、Run delegated cost 读取与 accounting drift 拒绝。完整 `pnpm check`、Go race 与真实 PostgreSQL integration 作为本阶段最终 Gate。
