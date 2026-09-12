# Console Run Results Alpha

Run Explorer 现在把已有受保护 execution 结果能力收口成一个可消费的人类工作流：Run detail、Event timeline、Artifact metadata 并行读取；Event 使用服务端签名 cursor 分页；Artifact 内容只在用户显式选择后按需读取。

## Console 行为

- `/runs` 继续先通过 Browser Session + CSRF 申请短期 Human Run delegation；OIDC Cookie 不直接访问 execution API。
- delegation token 只在当前页面内存中，React Query key 只保存本地 `sessionKey`、Workspace、Run/Artifact ID 等非秘密标识。
- Run detail、Event 第一页和 Artifact metadata 是独立 Query，可并行请求，不为 Artifact 内容制造前置 waterfall。
- “加载更多事件”把上一页 `next_cursor` 与同一 `through_version` 交回 API client；当前 traversal 不混入之后产生的新事件。
- Artifact metadata 列表不会自动拉取业务结果。用户选择具体 Artifact 后才请求 `/artifacts/{artifact_id}`，并用 React `<pre>` 文本渲染 JSON，不使用 HTML 注入。
- 断开短期访问会取消并清理 Run、Event、Artifact metadata 和 Artifact content Query cache。

## 边界

当前仅查看 <=1 MiB 的 `provider_result` JSON。没有对象存储、任意文件下载、SSE timeline、Artifact 删除，也没有把 provider control、credential 或结算内部字段加入返回 envelope。
