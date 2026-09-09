# Mender

让 Agent 通过统一入口发现、授权和调用工具。

Mender 为 Agent 开发者与团队提供统一的工具接入与管理入口，连接 HTTP API、MCP 工具和远程 Agent，集中管理授权、执行记录与调用费用。

> 开发中：当前可本地预览 Console、Admin 和服务状态页，业务能力按[实施计划](docs/planning/README.md)推进。

## 产品规划

- **接入能力**：统一管理 API、MCP 工具和远程 Agent，维护版本与授权连接。
- **分发工具**：通过固定工具集或按需发现，让 Agent 获取适合当前任务的能力。
- **追踪执行**：查看任务状态、执行事件、结果和用量，处理异步任务与失败重试。
- **管理权限与费用**：按工作空间和调用身份控制访问范围、预算及审计记录。

## 本地预览

需要 Node.js `24.19.0`、pnpm `11.18.0` 和 Go `1.26.7`。

```sh
pnpm install --frozen-lockfile
pnpm dev:api
```

另开终端启动前端：

```sh
pnpm dev
```

访问 [Console](http://127.0.0.1:5173) 或 [Admin](http://127.0.0.1:5174)。环境配置和开发命令见[开发指南](docs/engineering/development.md)。

## 文档

- [产品与系统架构](docs/design/01_Mender_产品与系统架构设计.md)
- [实施路线与任务台账](docs/planning/README.md)
- [接口合同](contracts/README.md)
- [完整文档](docs/README.md)

## 许可证

[MIT](LICENSE)
