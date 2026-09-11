# Mender Backend

最新实现：[Commerce Usage Settlement](../docs/engineering/2026-09-11-commerce-usage-settlement.md)。Provider terminal result/cancel acknowledgement 与 Execution settlement job 同事务持久化，受限 settlement principal 再通过 `processes/settlement` 将 held reservation 原子转换为 Commerce consumed quota；固定 success-only 价格下 success 收取 immutable `charge_micro`，failed/canceled 零 charge 并释放 held quota。

已有受保护 Run 查询／取消与公共 StartRun、原子预算预留、Worker lease/fencing、Supplier submission、HTTP executor、Provider result/status/cancel 收敛及 quota/usage settlement。它仍不是 payment wallet、收入总账或真实收费系统；真实外部 Provider、MCP／Agent、Artifact、Webhook 和 Outbox external delivery 仍未开放。

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
internal/processes/settlement/   终态 Execution fact → Commerce quota 的受控同库 UoW
internal/platform/httpserver/    当前 Gin 探针适配
internal/sharedkernel/           最小共享纯类型预留
migrations/                     0001–0015：另含 Usage Settlement 与 terminal settlement jobs
tests/architecture/              Go AST 边界检查与负向 fixture
tests/execution/                 应用用例与仅测试编译的内存仓储
```

8 个上下文的 README 记录候选职责和所有权。bootstrap 只在显式配置与数据库角色自检通过后安装相应能力；Provider control 与 Usage Settlement 都保持受审 library composition，没有 ambient production 开关或默认调度循环。数据库测试真实执行仍不代表支付、人类登录、完整业务前端或生产可用。

测试内存仓储只存在于 `_test.go`，不会链接到 API／Worker。运行中的任务接受取消后保持 `cancel_requested`；结果不明时保持 `reconciling`，不把网络超时误认为远端已停止。

```sh
go test ./...
go vet ./...
go build ./...
```

API／Worker 共享生命周期装配，接收进程停止信号。Worker 空闲等待取消；没有用空循环、内存任务或虚构结果替代可靠任务消费。

默认仍是 probes-only，readiness 为 503。完整说明见 [开发指南](../docs/engineering/development.md)、[工程报告](../docs/engineering/initialization-report.md)、[execution 切片记录](../docs/engineering/2026-09-09-execution-foundation.md) 和 [身份持久化记录](../docs/engineering/2026-09-09-identity-postgres.md)。
