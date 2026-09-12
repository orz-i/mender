# Mender Console：Run Explorer Alpha

日期：2026-09-12。本切片首次把已经存在的受保护 Execution API 暴露为可用 Console 工作流；它不是人类身份／Workspace 管理系统，也不改变后端授权、取消或结算真源。

## 前端边界

Console 新增 `modules/execution`，继续遵守 `domain → application ← infrastructure / presentation` 的源码依赖方向：

- `domain` 只定义 Run、RunEvent、Artifact 投影和终态／可取消显示规则；不依赖 React、HTTP 或 API DTO。
- `application` 定义消费方 `RunGateway`，包含 list/get/events/artifacts/cancel；不依赖传输实现。
- `infrastructure` 是唯一依赖 `@mender/api-client` 的模块层，显式把 transport DTO 映射成本地模型。
- `presentation` 接收注入的 `RunGateway`，使用 React Query 管理服务器状态，不实例化 transport client。
- `app/router.tsx` 是 Composition Root，构造 gateway 并注入 `/runs` 页面。

架构检查会解析别名、相对路径、re-export、type-only import 与循环，因此 presentation 直接 import API client、domain 引入 React 或 infrastructure 反向依赖 presentation 都会失败。

## Alpha 凭据模型

现阶段没有人类 OIDC/session，Console 因此明确要求操作者输入当前 Workspace ID 与 machine API Key。该 Key：

- 只保存在当前 React 页面内存；连接后清空 password 输入框；刷新即丢失。
- 不写入 URL、React Query key、localStorage、sessionStorage 或文档。
- transport 显式使用 `credentials: omit`、`cache: no-store` 与 `referrerPolicy: no-referrer`，并只在 Authorization Bearer header 中携带 Key。
- “断开”会取消并移除 Run Query cache，然后删除内存 access 对象。

这仍只是开发 Alpha 的机器身份入口，不应被包装成人类登录。正式 Console 身份后续应由后端 session/OIDC 取代浏览器机器 Key。

## Run Explorer 能力

页面调用现有受保护 API：

- `GET /api/v1/workspaces/{workspace}/runs?limit=20&state=...`
- `GET /api/v1/workspaces/{workspace}/runs/{run}`
- `GET /api/v1/workspaces/{workspace}/runs/{run}/events?limit=100`
- `GET /api/v1/workspaces/{workspace}/runs/{run}/artifacts`
- `POST /api/v1/workspaces/{workspace}/runs/{run}/cancel`

列表支持状态过滤、刷新、next cursor 和回到第一页。选中 Run 后，详情、事件与 Artifact 元数据通过 `Promise.all` 并行读取，避免串行 waterfall。页面不读取 Provider control evidence，不读取 Artifact 内容，也不计算账务结果。

取消按钮只做 UX 层最小过滤：终态和已经 `cancel_requested` 的 Run 不再显示新的请求入口；真正能否安全取消仍由服务端协调取消／Provider cancel 合同裁决。`cancel_requested` 不展示为“已取消”。错误码和消息来自受保护 API，不从客户端猜测结果。

## API client

`@mender/api-client` 新增严格 Run transport：验证 Workspace/Run ID、已知状态、分页、事件与 Artifact 响应结构；错误响应转换为 `MenderApiError(status, code, message)`，不会把 Bearer token 拼入 URL 或错误消息。根 `pnpm test` 已纳入 `runs.test.mjs`。

## 当前限制

- 事件界面当前展示前 100 条并在存在 next cursor 时明确提示；后续再做完整事件分页。
- Artifact 只展示 metadata，不开放 content view/download。
- 无 Catalog／Connection／Toolset 自助管理，因此 Workspace 与 Key 仍由现有 operator 流程准备。
- 无浏览器 E2E 自动化和人类认证；本阶段以 typecheck/build、架构检查、API client unit tests、全仓门禁与真实 PostgreSQL 后端集成为验收依据。
