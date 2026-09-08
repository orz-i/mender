# catalog · 能力目录

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC02、设计 G02。

- 所有权：Tool、ToolVersion
- 职责：工具语义、输入输出合同、可见性
- 计划负责角色：BE-A（尚未指派实际负责人）
- 候选公开合同：ToolContract、PublishedToolVersion
- 边界：Provider 和价格只是引用／投影

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
