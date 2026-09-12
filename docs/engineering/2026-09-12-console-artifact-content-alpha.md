# Console Artifact 内容读取 Alpha

本阶段确认并收口现有 execution Artifact detail 能力，正式把同一受保护内容读取合同发布到 Human Run delegation 的 Console 路径。

## 已实现

- 机器入口：`/api/v1/workspaces/{workspace}/runs/{run}/artifacts/{artifact}`。
- Console delegated 入口：`/api/console/v1/workspaces/{workspace}/runs/{run}/artifacts/{artifact}`。
- 两个入口均先执行 Run `run:read` 授权，再按 Workspace + Run + Artifact ID 读取；浏览器 session cookie 不是 Run credential。
- 仅返回 execution-owned `provider_result`、`application/json`，内容最大 1 MiB。
- 应用层要求 `size_bytes` 与实际返回 JSON UTF-8 字节长度完全一致，并拒绝无效 UTF-8、无效 JSON、错误 Workspace/Run/Artifact 投影及越界大小。
- Artifact list 仍只返回 metadata，不把内容混入列表。
- Provider request/task id、source observation、credential、canonical arguments 与结算内部状态不是 API envelope 的字段。

## 明确未实现

对象存储、signed URL、文件上传、Artifact 删除、任意媒体类型、流式下载均不属于本 Alpha。Provider 返回的业务 JSON 本身属于结果数据；平台不会根据字段名猜测并删除合法业务字段。

合同更新为 `contracts/artifact-read.openapi.yaml` v0.2.0，并同时描述 Machine Key 与短时 Run delegation 两种入口。
