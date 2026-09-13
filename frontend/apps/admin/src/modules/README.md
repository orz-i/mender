# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。`service-status` 读取真实健康状态；`publication-review` 管理 maker/checker 审核；`publication-history` 只读消费服务端 append-only Governance audit timeline；`publication-policy` 创建/激活不可变声明式 PolicyRevision，并只展示服务端 PolicyDecision。CSRF 只在 mutation infrastructure 读取，presentation 不根据 Workspace role 或 ToolVersion 属性自行推断 reviewer/audit/policy 权限、风险或 allow/deny。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。Governance 页面不会展示 Connection secret、执行参数、Artifact body、quota/payment amount；任意脚本策略、运行时执行策略、复杂多级审批、通知、SIEM 和外部审计导出仍未接入。
