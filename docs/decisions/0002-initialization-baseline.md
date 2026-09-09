# INIT-002 · 初始化工程基线

日期：2026-09-08。状态：**当前工程基线**。领域边界与关键事务决策待 G0 评审。

## 当前选择

- 保留设计建议的模块化 Go API＋独立 Worker，Go 模块按上下文组织。仅实现探针与进程生命周期；没有定义租户、账本或任务表。
- 根目录为一个 pnpm workspace，Console／Admin 分别构建和启动，共享 UI、传输客户端和 TS 配置。使用 React Router Data Mode，真实服务状态请求经消费方端口和 TanStack Query 装配。
- 保留设计的 React／Vite／Tailwind／shadcn 技术方向；shadcn Button 从官方 MIT 源码适配，未引入远程业务 UI。
- 复用本机 Node `24.19.0`、pnpm `11.18.0`、Go `1.26.7`，直接依赖采用经安装与构建核验的精确版本；详见锁文件和初始化报告。
- TypeScript 采用 `6.0.3`，与 `typescript-eslint 8.69.0` 声明的 `<6.1.0` 范围相容。
- 当前数据库、缓存、对象存储均不接线，不生成未经验证的迁移或 Docker 部署。待持久化切片时选择并锁定相应版本。
- 开发 API 默认使用 `127.0.0.1:18080`，两端 Vite 代理保持一致，支持环境变量覆盖。
- 提供基础 GitHub Actions 检查；Go／TS 全量依赖图、事务负向样例和权限测试仍待实施。

## 参考与证据

工程用法核对了 [Vite 安装文档](https://vite.dev/guide/)、[pnpm 配置文档](https://pnpm.io/settings)、[Gin Quickstart](https://gin-gonic.com/en/docs/quickstart/)、[Tailwind Vite 集成](https://tailwindcss.com/docs/installation/using-vite) 和 [React Router Data Mode](https://reactrouter.com/start/data/installation)。精确包版本及 peer 范围读取 npm 官方注册表和 Go 模块元数据，最终解析由真实锁文件记录。

执行结果见[工程基线](../engineering/initialization-report.md)。
