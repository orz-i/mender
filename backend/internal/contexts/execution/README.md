# execution · 执行

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC05、设计 G02。

- 所有权：Run、Attempt；Artifact 元数据模块
- 职责：状态、幂等、租约、执行与产物
- 计划负责角色：BE-A／BE-B（尚未指派实际负责人）
- 候选公开合同：RunView、ExecutionOutcome
- 边界：执行状态与结算状态分开

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
