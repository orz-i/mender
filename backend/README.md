# Mender Backend

最新实现：[Provider HTTP Control Runtime](../docs/engineering/2026-09-11-provider-control-runtime.md)。在 durable Provider Result／Reconciliation／Cancellation 语义上新增固定 status/cancel endpoint、hardened HTTP control adapter 与 reviewed executor/reconciler 双角色 composition；默认 API/Worker 没有生产 SecretProvider 或 provider-control host，因此仍 fail closed、不访问真实供应商。

已有受保护 Run 查询／取消与公共 StartRun、原子预算预留、Worker lease/fencing、Supplier submission、HTTP executor、Provider result/status/cancel 收敛。真实外部 Provider、MCP／Agent、结算、Artifact 和 Outbox external delivery 仍未开放。

独立 Go 模块 `github.com/orz-i/mender/backend`。从仓库根可用 `pnpm dev:api`／`pnpm dev:worker`，或在本目录直接使用 Go。

```text
cmd/api/                         API 进程入口
cmd/worker/                      Worker 进程入口
cmd/operator/                    显式迁移／角色授权／机器 Key 操作员入口
internal/bootstrap/              配置、生命周期与显式装配
internal/contexts/<context>/     候选领域边界
  domain/                        Run、机器凭据及 commerce 额度聚合
  application/                   领域用例与消费方端口
  adapters/inbound|outbound/      identity 与 execution 已有 HTTP／PostgreSQL／凭据适配
  public/                        身份、受理和预留的稳定集成合同
internal/processes/admission/    内部原子受理用例、事务端口及防腐层
internal/platform/httpserver/    当前 Gin 探针适配
internal/sharedkernel/           最小共享纯类型预留
migrations/                     0001–0013：identity／execution／commerce／supplier runtime 与 Provider control
tests/architecture/              Go AST 边界检查与负向 fixture
tests/execution/                 应用用例与仅测试编译的内存仓储
```

8 个上下文的 README 记录候选职责和所有权。bootstrap 只在显式配置与数据库角色自检通过后安装相应能力；Provider control 还要求代码注入受审 Provider/egress/SecretProvider，不存在 ambient production 开关。数据库测试真实执行仍不代表结算、人类登录、完整业务前端或生产可用。

测试内存仓储只存在于 `_test.go`，不会链接到 API／Worker。运行中的任务接受取消后保持 `cancel_requested`；结果不明时保持 `reconciling`，不把网络超时误认为远端已停止。

```sh
go test ./...
go vet ./...
go build ./...
```

API／Worker 共享生命周期装配，接收进程停止信号。Worker 空闲等待取消；没有用空循环、内存任务或虚构结果替代可靠任务消费。

默认仍是 probes-only，readiness 为 503。完整说明见 [开发指南](../docs/engineering/development.md)、[工程报告](../docs/engineering/initialization-report.md)、[execution 切片记录](../docs/engineering/2026-09-09-execution-foundation.md) 和 [身份持久化记录](../docs/engineering/2026-09-09-identity-postgres.md)。
