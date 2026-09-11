# Mender Backend

最新实现：[Fixed Toolset MCP Distribution Foundation](../docs/engineering/2026-09-11-fixed-toolset-mcp.md)。在原有四个 MCP 平台元工具之外，API 可显式启用固定 Toolset endpoint，把已审核的 immutable ToolVersion + fixed Connection binding 直接映射为 MCP `tools/list` / `tools/call` 业务工具；协议仍固定官方 MCP Go SDK v1.7.0 与 `2026-07-28` stateless Streamable HTTP。

已有受保护 Run/Artifact 查询、取消与公共 StartRun、原子预算预留、Worker lease/fencing、Supplier submission、HTTP executor、Provider result/status/cancel 收敛及 quota/usage settlement。MCP 已包含平台元工具与固定 Toolset direct tools；上游 MCP Client、OAuth、Resources/Prompts、MRTR 和 Agent 仍未开放。Artifact v1 只支持 <=1 MiB inline JSON；Commerce 仍不是 payment wallet 或真实收费系统。

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
internal/processes/mcpbridge/    MCP 纯用例、跨域 ACL 与官方 SDK inbound adapter
internal/platform/httpserver/    当前 Gin 探针适配
internal/sharedkernel/           最小共享纯类型预留
migrations/                     0001–0018：另含 Artifact、Tool Contract 与 Fixed Toolset 发布兼容
tests/architecture/              Go AST 边界检查与负向 fixture
tests/execution/                 应用用例与仅测试编译的内存仓储
tests/mcp/                       MCP 2026-07-28、元工具与 Fixed Toolset direct tools 协议测试
```

8 个上下文的 README 记录候选职责和所有权。bootstrap 只在显式配置与数据库角色自检通过后安装相应能力。MCP gateway 和 Fixed Toolset 分发都默认关闭；Fixed Toolset 只有在 gateway 已启用时才能打开。Bearer machine Key 在进入 SDK 前与 path Workspace 交叉验证；没有 run:create 的有效凭据完成协议 discovery 但得到空业务 Tool surface，真正 `tools/call` 会再次授权并走现有 Admission。数据库/MCP E2E 测试真实执行仍不代表支付、人类登录、完整业务前端或生产可用。

测试内存仓储只存在于 `_test.go`，不会链接到 API／Worker。运行中的任务接受取消后保持 `cancel_requested`；结果不明时保持 `reconciling`，不把网络超时误认为远端已停止。

```sh
go test ./...
go vet ./...
go build ./...
```

API／Worker 共享生命周期装配，接收进程停止信号。Worker 空闲等待取消；没有用空循环、内存任务或虚构结果替代可靠任务消费。

默认仍是 probes-only，readiness 为 503。完整说明见 [开发指南](../docs/engineering/development.md)、[工程报告](../docs/engineering/initialization-report.md)、[execution 切片记录](../docs/engineering/2026-09-09-execution-foundation.md) 和 [身份持久化记录](../docs/engineering/2026-09-09-identity-postgres.md)。
