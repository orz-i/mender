# Run Event 分页 Alpha

execution 后端已经使用签名 cursor 对事件时间线做有限分页；本阶段把该语义正式暴露给 Console/API client，并补齐 Human Run delegation 合同。

## 一致性语义

- 第一页读取当前 Run revision 作为 `through_version`。
- 后续 cursor 绑定 Workspace、调用主体、credential/delegation、Run、page size、`after` 与 `through_version`，15 分钟过期。
- 后续新事件不会混入当前 traversal；用户刷新时重新从第一页读取才能看到更高 revision。
- 事件按 revision 严格递增；客户端拒绝倒序、重复、超过 watermark 或跨页 `through_version` 漂移。
- HTTP 不支持 offset，也不允许 events 使用 state filter。

Console API client 不再固定只请求 `limit=100` 第一页；现在支持显式 `limit`、`cursor` 与预期 watermark 校验。该分页不是 SSE、长轮询或 Outbox consumer。
