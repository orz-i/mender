# Mender：公开 StartRun 与真实计划解析

日期：2026-09-10。本轮在既有原子受理和协调取消基础上开放**显式配置后的**公开 StartRun。实现仍停在“安全受理并创建 blocked Job”，不包含真实 Worker、供应商网络调用、OAuth refresh、Outbox 投递或支付结算。

## 实现边界

`POST /api/v1/workspaces/{workspace_id}/runs` 默认不注册。启用要求 `MENDER_RUN_API_ENABLED=true`、`MENDER_RUN_START_API_ENABLED=true`，并同时提供普通受限读连接 `MENDER_DATABASE_URL` 与同库不同角色的 `MENDER_ADMISSION_DATABASE_URL`。启动会验证迁移摘要、两类角色权限及数据库一致性；错误配置失败关闭，不回退到内存仓储或管理账号。

机器 Key 新增 `run:create` scope。HTTP 入口先认证，再绑定 path Workspace，随后才解码并授权创建；重复/未知 JSON 字段、重复嵌套字段、非 JSON media type、重复 Idempotency-Key、超大 body 和参数对象中的重复 Key 都会拒绝。`Idempotency-Key` 持久边界与 OpenAPI 统一为 8–128 字符。

客户端只提交 ToolRef、Toolset 标识、Connection、arguments、币种与最大费用。服务端按顺序解析：Workspace 已发布 Toolset binding → 精确不可变 ToolVersion → 当前主体对 Connection 的授权及 provider 匹配 → ToolVersion 绑定的 PriceVersion → Toolset 绑定预算的唯一当前 Budget period。客户端不能指定 `tool_version_id`、`price_version_id`、`budget_id` 或 `period_id`。

计划读取遵守 DDD 所有权：Catalog、Distribution、Connections、Commerce 各自拥有 domain/application/public/adapter；admission process 只消费公开端口。普通 API 角色可读取解析所需字段，但不能读取 Connection 内部 credential version reference，也不能读取预算 limit/consumed/reserved 数值。独立 admission writer 只能执行原子额度预留及 append-only Run/Admission/Job/Outbox 写入，不能读取或改写 Catalog、Distribution、Connections、PriceVersion 源事实。

受理继续复用既有单事务 UoW：验证幂等记录后，锁定预算期、保留额度、写 Run、不可变 admission snapshot、`blocked / executor_not_configured` Job 和最小 Outbox 事件。Outbox 不包含 arguments。当前未配置 executor，因此 HTTP 成功仅返回 `queued` + `reserved`，不会声称外部供应商结果或执行完成。

## 运维配置

迁移仍由独立管理员连接执行，不由 API startup 自动执行。管理员预先创建无特权 login role 后，可用：

```sh
pnpm db:migrate
pnpm db:grant-runtime --role mender_app
pnpm db:grant-admission --role mender_admission
pnpm key:issue --workspace <workspace> --subject <service-account> --scopes run:read,run:create
```

随后安全注入 `MENDER_DATABASE_URL`、`MENDER_ADMISSION_DATABASE_URL`，再显式打开两个 Run 开关。不得复用迁移 owner、superuser、runtime reader、admission writer 或 cancellation writer。非回环 PostgreSQL 仍要求已验证 TLS；本轮没有公网部署授权。

Catalog/Toolset/Connection/Price/Budget 的正式管理 API 与后台 UI 尚未实现；真实环境数据应由后续各 bounded context 的管理用例写入，不应通过 API startup seed 或手工绕过领域约束。本轮测试只在一次性隔离数据库中构造受控 fixture。

## 验证证据

焦点 Go 测试覆盖 Resolver 顺序与 fail-closed、`run:create` 授权、HTTP 认证/Workspace/idempotency/strict JSON/body limit，以及 bootstrap 默认关闭和配置失败。真实 `node scripts/test-postgres.mjs` 隔离套件已执行通过并清理自有容器；其中生产 bootstrap 真实走过 Identity、RLS Toolset、Catalog ToolVersion、Connection grant、PriceVersion、Budget period、原子 reservation/Run/admission/blocked Job/Outbox，以及 replay、同 Key 改请求冲突、只读 Key 拒绝、Connection revoke 和 reader/writer 权限隔离。

正式 WBS、ADR-020 和 G0–G5 状态不因本轮代码及测试自动改为验收完成。下一阶段应实现 Worker lease/fencing/attempt 生命周期和可恢复调度；在此之前不得把 blocked Job 改成可执行任务，也不得接入真实供应商或资金结算。
