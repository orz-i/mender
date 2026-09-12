# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。`service-status` 提供真实健康请求；`execution` 提供 Run Explorer；`workspace-access` 使用 HttpOnly OIDC browser session 获取用户和成员关系；`connection-access` 通过独立服务端权限列出安全 Connection 投影，允许 Owner/Admin 发起 CSRF 保护的撤销，并可启动服务端 reviewed OAuth 创建流程。Provider endpoint、scope 与 provider credential 都不会由 presentation/domain 持有。CSRF cookie 只在 infrastructure 读取并回显给状态变更请求。不预建通用 features 包。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。Run Explorer 现在从 HttpOnly OIDC session 显式申请短时 Run delegation，token 只保存在页面内存，不能进入 URL、Query Key、localStorage 或 sessionStorage；OIDC Cookie 本身不直接访问 Run API，最终授权、状态与取消裁决始终来自后端。
