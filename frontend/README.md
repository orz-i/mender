# Mender Frontend

使用仓库根 pnpm workspace 与唯一 `pnpm-lock.yaml`。Console 和 Admin 分入口运行、独立构建，均为初始化预览。

| 工作区 | 职责 |
| --- | --- |
| `@mender/console` | 消费者／发布者入口；目前只有首页和服务状态 |
| `@mender/admin` | 平台管理入口；尚未接入身份或管理操作 |
| `@mender/ui` | shadcn Button、共享 Shell 和 CSS tokens；无业务模型 |
| `@mender/api-client` | 手写健康请求传输客户端和响应检查；不是完整业务 SDK |
| `@mender/config` | 共享严格 TypeScript 配置，无运行时业务逻辑 |

应用 `app/` 为装配根；`modules/service-status/` 演示领域投影、消费方端口、HTTP 映射与 React Query 页面。两个应用不深导入彼此代码；暂不为很小的展示用例增加共享业务包。

启动与验证命令见 [开发指南](../docs/engineering/development.md)。未来 shadcn 组件在 `packages/ui` 中维护，配置文件为 [components.json](packages/ui/components.json)，公开消费入口为 `@mender/ui`。当前样式是初始化工作界面的最小视觉基线；交付包没有提供可直接逐像素复刻的完整 UI 制品。

Button 基于 shadcn/ui 的 new-york 源码适配，保留 [上游 MIT 许可证](packages/ui/SHADCN_LICENSE.md)。

生产身份隔离、完整业务路由、Workspace 缓存隔离、权限处理与领域边界扫描仍按 S0／S1 实施。现有两端构建通过不代表这些功能已完成。
