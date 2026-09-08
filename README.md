# Mender

Mender 是面向 Agent 的能力集成、治理与分发平台，设计覆盖 HTTP API、远程 MCP Tools 和远程 Agent API。项目已由 **MCPX 更名为 Mender**。

当前交付为 **项目初始化骨架**：Go API／Worker、独立的 Console／Admin、pnpm 工作区、合同校验、基础 CI 与文档归档。登录、租户、工具调用、任务队列和计费尚未实现；设计包中的 G0／G1 等验收门禁尚未签署。

## 本地启动

工具链：Node.js `24.19.0`、pnpm `11.18.0`、Go `1.26.7`。精确版本和证据见 [工具链清单](toolchain.versions.json) 与 [开发指南](docs/engineering/development.md)。

```sh
pnpm install --frozen-lockfile
pnpm dev:api
```

另开终端启动两个前端入口：

```sh
pnpm dev
```

- Console：[http://127.0.0.1:5173](http://127.0.0.1:5173)
- Admin：[http://127.0.0.1:5174](http://127.0.0.1:5174)
- API 存活探针：[http://127.0.0.1:18080/healthz](http://127.0.0.1:18080/healthz)

`pnpm dev:worker` 可单独启动空闲 Worker；尚未消费任务。前端“服务状态”请求真实 API，失败时可重试。`/readyz` 当前返回 `503 not_ready`，以区分进程存活与业务就绪。启动骨架无需数据库、Redis 或云服务。

```sh
pnpm check
```

该命令检查工具链、工作区、归档与文档链接、合同结构、lint、类型、传输测试、Go 测试／vet／构建及两个前端构建。它不是完整 DDD 门禁或产品验收。

## 仓库导航

| 目录 | 用途 |
| --- | --- |
| [backend](backend/README.md) | Go 独立模块；API／Worker、bootstrap、8 个候选上下文 |
| [frontend](frontend/README.md) | Console／Admin 和共享 UI、API client、配置包 |
| [contracts](contracts/README.md) | Mender 标题下的业务合同设计样例；尚未作为服务实现 |
| [docs](docs/README.md) | 设计、计划、工程治理、决策与原始交付归档 |
| [scripts](scripts/README.md) | 可复现的本地检查与 Go 命令入口 |
| [.github/workflows/ci.yml](.github/workflows/ci.yml) | 基础检查工作流，无发布或部署动作 |

从 [文档索引](docs/README.md) 阅读设计；从 [初始化报告](docs/engineering/initialization-report.md) 查看已实现范围、验证证据和后续工作。原始文件保持字节不变，更名映射见 [项目更名决策](docs/decisions/0001-project-name.md)。

许可证：[MIT](LICENSE)。
