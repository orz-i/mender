# identity · 身份与租户

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC01、设计 G02。

- 所有权：Workspace、Membership、ServiceAccount、APIKey
- 职责：成员和调用主体、Key 生命周期
- 计划负责角色：BE-A（尚未指派实际负责人）
- 候选公开合同：身份与 Workspace ID／授权事实
- 边界：平台管理员与租户管理员分离

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
