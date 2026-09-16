# Mender Frontend

使用仓库根 pnpm workspace 与唯一 `pnpm-lock.yaml`。Console 和 Admin 分入口运行、独立构建。已注册业务路由与完整产品验收是两个状态；最新范围见[当前实施与验收状态](../docs/planning/current-status.md)。

| 工作区 | 职责 |
| --- | --- |
| `@mender/console` | 已有 Workspace、Connection、Catalog、Launch、Usage、Run 路由；新的发布者工作台仍待交付 |
| `@mender/admin` | 已有 Catalog publication review/history/policy 与 execution governance；S4 发布、JIT、异常和财务界面仍未全量交付 |
| `@mender/ui` | shadcn Button、共享 Shell 和 CSS tokens；无业务模型 |
| `@mender/api-client` | 健康与现有业务 DTO 的受控传输客户端；不是完整外部 SDK／CLI |
| `@mender/config` | 共享严格 TypeScript 配置，无运行时业务逻辑 |

应用 `app/` 为装配根，各业务 `modules/` 维持领域投影、消费方端口、HTTP 映射与 React 页面边界。实际路由以 [Console router](apps/console/src/app/router.tsx) 和 [Admin router](apps/admin/src/app/router.tsx) 为准，两个应用不深导入彼此代码。

启动与验证命令见 [开发指南](../docs/engineering/development.md)。未来 shadcn 组件在 `packages/ui` 中维护，配置文件为 [components.json](packages/ui/components.json)，公开消费入口为 `@mender/ui`。当前样式是初始化工作界面的最小视觉基线；交付包没有提供可直接逐像素复刻的完整 UI 制品。

Button 基于 shadcn/ui 的 new-york 源码适配，保留 [上游 MIT 许可证](packages/ui/SHADCN_LICENSE.md)。

前端解析后依赖检查与负向样例已接入工程检查；现有路由和客户端测试不能替代浏览器可用性、生产身份、全部权限以及剩余 S4 工作台验收。后续按原 S4-09／10／11 和 S4-13／14 补齐，不另增阶段。
