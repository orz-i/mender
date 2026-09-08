# supply · 供应与发布

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC07、设计 G02。

- 所有权：Provider、PluginVersion、Deployment、ReleasePlan
- 职责：供应商入驻、制品、审核与发布
- 计划负责角色：BE-B（尚未指派实际负责人）
- 候选公开合同：ProviderProfile、DeploymentDescriptor
- 边界：runtime 是此领域的外部执行机制

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
