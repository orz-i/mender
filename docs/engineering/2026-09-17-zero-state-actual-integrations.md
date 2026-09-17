# 从空 Workspace 到真实集成：可用性审计与验收目标

日期：2026-09-17

这份记录针对用户反馈“无法创建，也无法使用”。本轮不再以预置 `Company lookup` demo 作为产品可用性的证明，而以一个**完全空的 Workspace** 能否由普通 Workspace Owner 完成真实集成创建和调用作为验收口径。

## 已复现的产品断点

- `GET /api/console/v1/workspaces` 只能列出现有 membership；没有用户自助创建 Workspace 的 HTTP 入口。
- Connections 页面只能走一个运维预配置的 OAuth Provider；没有 BYOK/API Key 创建路径。
- Publisher/Catalog 虽然能创建 ToolVersion 和 Toolset 草稿，但要求用户先知道 `provider_id`、`deployment_revision`、`price_version_id`、`connection_id`、`budget_id` 等内部 ID。
- Provider deployment、PriceVersion、Budget period 没有面向 Console 的安全自助创建合同，因此普通用户无法从零把 Catalog 草稿变成真正可运行的 Tool。
- 当前 HTTP supplier runtime 接受的是 Mender 自己的 provider submit/status JSON 协议，并不能直接把任意第三方 REST API 当作同步 Tool 调用。
- 当前 upstream MCP runtime 只认证 `2026-07-28` stateless MCP；AnySearch 公布的原生 Streamable HTTP 服务目前使用 `2025-03-26` 兼容面，因此不能直接接入。

因此，“页面上有创建表单”和“用户可以创建可运行能力”不是一回事。本轮必须补齐真实安装/运行合同，而不是继续给空状态增加按钮。

## 三个真实验收对象

### OpenAI Image generation

官方 API：`POST https://api.openai.com/v1/images/generations`

资料：<https://developers.openai.com/api/reference/resources/images/methods/generate>

Mender 验收要求：

- 用户只选择 “OpenAI” 并录入自己的 API Key；浏览器不保存、不回显 Key。
- Mender 使用固定审核过的 OpenAI endpoint，不允许用户输入任意 URL。
- 安装完成后 Tools 页面出现 Image generation，并能真实调用 OpenAI。
- 返回图片必须能作为 Run result / Artifact 查看，不要求用户理解 Provider/Deployment revision。

### OpenAI Speech generation

官方 API：`POST https://api.openai.com/v1/audio/speech`

资料：<https://developers.openai.com/api/reference/resources/audio/subresources/speech/methods/create>

Mender 验收要求：

- 复用同一个 OpenAI Connection/API Key。
- 用户输入文本、模型/voice 的受控选项后可以得到真实音频结果。
- 二进制结果通过 Mender 的结果/Artifact 边界返回，不把 Base64/内部存储细节暴露成主交互。

### AnySearch MCP

官方 MCP endpoint：`https://api.anysearch.com/mcp`

资料：<https://www.anysearch.com/docs> 和 <https://github.com/anysearch-ai/anysearch-mcp-server>

AnySearch 当前支持匿名访问（较低限额），也支持 Bearer API Key。其公开说明以 MCP Streamable HTTP `2025-03-26` 为兼容基线。

Mender 验收要求：

- 用户可以先用 anonymous 模式连接，不需要在聊天或代码中保存 API Key。
- Mender 实际执行 MCP discovery，展示上游返回的 Tool，而不是写死假工具。
- 用户选择/安装 Tool 后，从 Console 发起 Run，经 Mender Worker 调到 AnySearch MCP，并在 Runs/Usage 查看结果。

## 产品化约束

三个实例都通过**服务端审核模板**安装。用户只提供业务必要信息（例如 OpenAI API Key），不能自行提交任意 egress host、数据库 ID 或内部 revision。

模板安装可以创建/绑定内部 Provider、Deployment、Connection、Price、Budget、ToolVersion、Toolset 等事实，但这些内部对象属于 Mender 实现细节；Console 默认只显示 Integrations、Tools、Runs、Usage。

OpenAI API Key 与可选 AnySearch Key 必须进入 Credential Vault，不能写入数据库明文字段、前端 storage、日志、Git 或文档。无 Key 的 AnySearch 使用 `auth_mode=none`。

真实 OpenAI 调用只有在用户通过本地 HTTPS 页面自行录入 Key 后才允许发生；自动测试使用本地协议 fixture，不产生 OpenAI 费用。

## 验收状态

本文件只记录审计与目标，不代表上述能力已经完成。完成必须由后续实现、真实浏览器流程和独立回归证据证明。
