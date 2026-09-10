# supply · 供应与发布

状态：G0 候选上下文，已落地供应商运行时输入/Deployment 边界；正式治理状态仍待冻结。来源：BC07、设计 G02。

- 所有权：Provider、PluginVersion、Deployment、ReleasePlan，以及供应商执行前的运行时材料编排
- 职责：供应商入驻、制品、审核与发布；组合 Execution immutable admission input、Connections credential reference 与外部 SecretProvider
- 计划负责角色：BE-B（尚未指派实际负责人）
- 候选公开合同：ProviderProfile、DeploymentDescriptor
- 边界：runtime 是此领域的外部执行机制

当前实现不会把 credential secret 写入数据库、RunEvent、Attempt 或日志。`supply.deployments` 只保存非秘密 transport 描述；执行角色只读取 opaque `credential_version_ref`，真正 secret 只能经 `SecretProvider` 在内存中取得。现有 Worker lease role 不获得这些读取权限。

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
