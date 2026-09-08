# connections · 授权连接

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC03、设计 G02。

- 所有权：Connection
- 职责：授权范围、连接状态、秘密引用
- 计划负责角色：BE-A（尚未指派实际负责人）
- 候选公开合同：ConnectionCapability、凭据代理端口
- 边界：不对消费者暴露原始全局秘密

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
