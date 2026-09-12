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

## 当前凭据模型

最初 Alpha 使用页面内存中的 machine API Key；Human→Run delegation 切片已经替换 Console 的手工 Key 表单。当前流程为：

- 用户先通过 HttpOnly OIDC browser session 进入 `/workspaces`，再把非秘密 Workspace ID 带入 `/runs`。
- Run Explorer 使用 same-origin session + CSRF 显式申请 1–30 分钟的 opaque delegation；Viewer 只申请 `run:read`，Owner/Admin/Developer 可以申请 `run:read` + `run:cancel`。
- delegation token 只保存在当前 React 页面内存，不写入 URL、React Query key、localStorage、sessionStorage 或 Cookie。
- 真正读取/取消 Run 时，transport 使用 `/api/console/v1/...` + explicit Bearer delegation，并设置 `credentials: omit`；OIDC Cookie 本身不会被发送到 Run endpoint 作为执行凭据。
- 显式“撤销短期访问”通过 browser session + CSRF 删除 delegation；标签页异常关闭时不能保证撤销请求完成，因此服务端 TTL 仍是安全边界。

Machine `/api/v1/...` surface 继续存在给 Agent/服务身份使用，但 Console 不再要求用户粘贴 Machine Key。

## Run Explorer 能力

Console 页面调用受委托保护的 API：

- `GET /api/console/v1/workspaces/{workspace}/runs?limit=20&state=...`
- `GET /api/console/v1/workspaces/{workspace}/runs/{run}`
- `GET /api/console/v1/workspaces/{workspace}/runs/{run}/events?limit=100`
- `GET /api/console/v1/workspaces/{workspace}/runs/{run}/artifacts`
- `POST /api/console/v1/workspaces/{workspace}/runs/{run}/cancel`

列表支持状态过滤、刷新、next cursor 和回到第一页。选中 Run 后，详情、事件与 Artifact 元数据通过 `Promise.all` 并行读取，避免串行 waterfall。页面不读取 Provider control evidence，不读取 Artifact 内容，也不计算账务结果。

取消按钮只做 UX 层最小过滤：终态和已经 `cancel_requested` 的 Run 不再显示新的请求入口；真正能否安全取消仍由服务端协调取消／Provider cancel 合同裁决。`cancel_requested` 不展示为“已取消”。错误码和消息来自受保护 API，不从客户端猜测结果。

## API client

`@mender/api-client` 新增严格 Run transport：验证 Workspace/Run ID、已知状态、分页、事件与 Artifact 响应结构；错误响应转换为 `MenderApiError(status, code, message)`，不会把 Bearer token 拼入 URL 或错误消息。根 `pnpm test` 已纳入 `runs.test.mjs`。

## 当前限制

- 事件界面当前展示前 100 条并在存在 next cursor 时明确提示；后续再做完整事件分页。
- Artifact 只展示 metadata，不开放 content view/download。
- Connection 已有安全元数据列表/撤销以及单一 reviewed OAuth 创建 Alpha；BYOK ingestion、refresh/rotation 和 Toolset/Catalog 自助仍待后续。
- Human delegation 当前只有 read/cancel，没有 `run:create`、billing authority 或 Toolset mutation。
- 无完整浏览器 E2E 自动化；本阶段以 typecheck/build、架构检查、API client unit tests、全仓门禁与真实 PostgreSQL 后端集成为验收依据。
