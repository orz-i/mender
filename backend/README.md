# Mender Backend

最新实现：[协调取消与额度释放](../docs/engineering/2026-09-10-coordinated-cancellation.md)。通过独立开关和受限连接对未执行的受理任务原子取消；未启用仍保持旧保护。操作员命令 `pnpm db:grant-cancellation --role mender_cancel` 仅为预先存在的角色授予权限。

已有 Run 列表／事件查询和受保护查询取消；本轮新增内部原子受理、额度预留、blocked Job 和待投递 Outbox，并在隔离 PostgreSQL 验证。没有公共创建或上游执行入口。当前边界见 [原子受理记录](../docs/engineering/2026-09-09-atomic-admission.md)。

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
migrations/                     0001–0005：identity／execution／commerce 与摘要验证
tests/architecture/              Go AST 边界检查与负向 fixture
tests/execution/                 应用用例与仅测试编译的内存仓储
```

8 个上下文的 README 记录候选职责和所有权。bootstrap 仅在显式配置与安全检查通过后注册已有 Run HTTP 子集。内部 BuildAdmission 必须显式注入创建授权与不可变计划解析端口；本轮只有测试 fixture，没有给运行 API 安装这些端口。数据库测试真实执行，仍不代表付费受理、结算、人类登录、业务前端或生产可用。

测试内存仓储只存在于 `_test.go`，不会链接到 API／Worker。运行中的任务接受取消后保持 `cancel_requested`；结果不明时保持 `reconciling`，不把网络超时误认为远端已停止。

```sh
go test ./...
go vet ./...
go build ./...
```

API／Worker 共享生命周期装配，接收进程停止信号。Worker 空闲等待取消；没有用空循环、内存任务或虚构结果替代可靠任务消费。

默认仍是 probes-only，readiness 为 503。完整说明见 [开发指南](../docs/engineering/development.md)、[工程报告](../docs/engineering/initialization-report.md)、[execution 切片记录](../docs/engineering/2026-09-09-execution-foundation.md) 和 [身份持久化记录](../docs/engineering/2026-09-09-identity-postgres.md)。
