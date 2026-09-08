# distribution · 工具集分发

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC04、设计 G02。

- 所有权：Toolset、PublishedSnapshot
- 职责：绑定、版本固定、入口与环境
- 计划负责角色：BE-B（尚未指派实际负责人）
- 候选公开合同：ToolsetSnapshot、路由解析结果
- 边界：不持有 Tool／Connection 聚合对象

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
