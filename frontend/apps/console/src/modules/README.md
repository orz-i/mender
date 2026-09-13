# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。`service-status` 提供真实健康请求；`execution` 提供 Run Explorer；`workspace-access` 使用 HttpOnly OIDC browser session 获取用户和成员关系；`connection-access` 管理安全 Connection 投影与 reviewed OAuth；`catalog-management` 管理 Workspace-scoped ToolVersion/Toolset draft，并只消费服务端 preflight/publish 裁决；`tool-launch` 消费服务端过滤后的 launch options，并通过短时 exact StartRun delegation 受理 Run；`usage-observability` 只读展示 Commerce quota 的 Budget/Usage 安全投影。Provider endpoint、scope、provider credential 和 delegated token 都不会由 domain 持有；敏感 token 仅作为 application 层短时工作流数据保存在当前页面内存。CSRF cookie 只在 infrastructure 读取并回显给状态变更请求。不预建通用 features 包。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。Run Explorer 现在从 HttpOnly OIDC session 显式申请短时 Run delegation，token 只保存在页面内存，不能进入 URL、Query Key、localStorage 或 sessionStorage；OIDC Cookie 本身不直接访问 Run API，最终授权、状态与取消裁决始终来自后端。
