# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。`service-status` 读取真实健康状态；`publication-review` 使用 Browser Session 获取 Workspace，再通过独立 Admin Governance API 列出/批准/拒绝精确 revision 的发布请求；`publication-history` 只读消费服务端 append-only Governance audit timeline，采用有界 sequence cursor 与明确过滤，不从当前 Approval 行推断历史。CSRF 只在 mutation infrastructure 读取，presentation 不根据 Workspace role 自行推断 reviewer/audit 权限。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。审计页面不会展示 Connection secret、执行参数、Artifact body、quota/payment amount；复杂多级审批、通知、SIEM 和外部审计导出仍未接入。
