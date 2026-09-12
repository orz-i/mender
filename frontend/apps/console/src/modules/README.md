# 前端模块

按用户任务组织模块，采用 domain / application / infrastructure / presentation。`service-status` 提供真实健康请求；`execution` 已提供最小 Run Explorer：应用层声明 RunGateway，infrastructure 映射受保护 Run API，presentation 使用 React Query 并行读取列表／详情／事件／Artifact 元数据并请求取消。后续目录、连接等模块按实际用例加入，不预建通用 features 包。

模块公开入口为 index.ts。app 负责装配；领域与应用层保持纯 TypeScript，不导入 React、Router、Query 或 API DTO。Run Explorer 的机器 Key 只保存在页面内存，不能进入 URL、Query Key、localStorage 或 sessionStorage；最终授权、状态与取消裁决始终来自后端。
