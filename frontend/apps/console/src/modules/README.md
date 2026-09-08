# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。当前 service-status 仅演示真实健康请求、传输映射与显式端口装配。后续目录、执行、连接等模块按实际用例加入，不预建通用 features 包。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。
