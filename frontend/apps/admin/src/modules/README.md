# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。`service-status` 读取真实健康状态；`publication-review` 管理 maker/checker 审核；`publication-history` 只读消费服务端 append-only Governance audit timeline；`publication-policy` 创建/激活不可变声明式 PolicyRevision，并只展示服务端 PolicyDecision；`execution-governance` 管理不可变 ExecutionPolicyRevision 并观察服务端执行风险决议与 Human confirmation 生命周期。CSRF 只在 mutation infrastructure 读取，presentation 不根据 Workspace role、ToolVersion 属性或参数内容自行推断 reviewer/audit/policy 权限、执行风险或 allow/deny。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。Governance 页面不会展示 Connection secret、原始执行参数、Artifact body、quota/payment amount；execution-governance 只展示服务端提供的 arguments hash 与治理元数据。任意脚本策略、参数内容规则、复杂多级审批、通知、SIEM 和外部审计导出仍未接入。
