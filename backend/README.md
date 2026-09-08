# Mender Backend

独立 Go 模块 `github.com/orz-i/mender/backend`。从仓库根可用 `pnpm dev:api`／`pnpm dev:worker`，或在本目录直接使用 Go。

```text
cmd/api/                         API 进程入口
cmd/worker/                      Worker 进程入口
internal/bootstrap/              配置、生命周期与显式装配
internal/contexts/<context>/     候选领域边界
  domain/                        纯领域模型预留
  application/                   消费方端口与用例预留
  adapters/inbound|outbound/      外部协议与持久化适配预留
  public/                        稳定公开合同预留
internal/processes/admission/    ADR-020 协调位置预留
internal/platform/httpserver/    当前 Gin 探针适配
internal/sharedkernel/           最小共享纯类型预留
migrations/                     尚未定义数据库迁移
tests/architecture/              完整架构门禁实施说明
```

8 个上下文的 README 记录候选职责和所有权；`doc.go` 仅使边界明确、可编译，没有业务行为。当前不对外提供设计合同中的消费 API。

```sh
go test ./...
go vet ./...
go build ./...
```

API／Worker 共享生命周期装配，接收进程停止信号。Worker 空闲等待取消；没有用空循环、内存任务或虚构结果替代可靠任务消费。

完整说明见 [开发指南](../docs/engineering/development.md) 和 [工程报告](../docs/engineering/initialization-report.md)。
