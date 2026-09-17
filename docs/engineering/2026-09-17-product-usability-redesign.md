# 2026-09-17 Web 产品可用性纠偏

## 触发原因

本地部署虽然通过健康检查、OIDC 和工程验证，但实际页面仍暴露 `Alpha`、Admission、ToolVersion 草稿、revision/hash 等内部术语；Console 的 Catalog 是工程表单，Admin 大量为空白工作台。用户截图证明“能部署”不等于“能使用”。

本轮不新增产品阶段，也不修改既有安全、授权、预算和账本语义。目标是把已有后端能力组织成普通用户可以理解和完成的产品流程。

## Web 组件体系

Web 端正式收敛到 `@mender/ui` 内的 shadcn/ui source components，使用 pnpm 管理 Radix 依赖。新增／规范化 Button、Card、Input、Textarea、Badge、Separator、Alert、Table、Empty、Field、Dialog、Select 和 Checkbox；Console/Admin 共用的 Workbench 也迁移到这些组件，不再维护原生 dialog/select/input 的第二套基础组件。

这不是只安装 shadcn CLI：组件源码、semantic tokens、类型检查和两个应用构建都纳入仓库。

## Console 信息架构

一级导航现在面向使用任务：Get Started、Tools、Connections、Runs、Usage；Workspace 下保留 Members & access、Publisher、Service status。

- 首页改为三步 onboarding：选工具 → 连接账号 → 运行并观察。
- `/catalog` 改为只读 Tools 浏览页，仅展示已发布能力；OpenAPI 导入、ToolVersion draft、Toolset 管理移动到 `/publisher/catalog`。
- Run 页面会把简单 JSON Schema 映射为正常表单；价格、风险和结果是主要信息，policy revision 和 arguments hash 进入 Advanced。
- Tools／Run／Usage 的空态、加载态和错误态使用 shadcn 组件，不再显示内部 Alpha 规划文案。

## Admin 信息架构

Admin 导航按 Review／Governance／Operations 分组。首页改为 Publication reviews、Releases、Execution governance、Billing 四个主要任务入口；Publication Review 页面改为 shadcn Card／Table／Badge／Select／Textarea 组合，空队列显示可理解的 Empty state。

服务端 maker/checker、CSRF、精确 revision、自审拒绝和独立数据库角色没有放宽；前端只改变信息呈现。

## 本地真实产品闭环

`deploy/local.demo.example.json` 现在显式启用 `local_demo_fixture`。它只能用于 `environment=local` + embedded local OIDC + `ws_local`，production 配置会拒绝。

fixture 包含：

- `Company lookup` / `company_lookup` read-only Tool；
- 仅 Worker loopback 可访问的 `local-provider`，无 secret、无 host port、无外部网络；
- Connection、PriceVersion、Budget、Toolset 与固定 deployment revision；
- 只在 `ws_local` 没有 active policy 时创建低风险 read-only execution policy；普通 Workspace 仍 fail-closed；
- reviewed Worker allowlist 只允许 `deploy_local_demo` / `provider_local_demo` / `127.0.0.1`。

持久实例升级前备份位于 `.local/backups/mender-demo-before-product-ui-20260917`，数据库卷没有删除。当前 release 为 `dist/releases/local-product-20260917-r2`。

## 实际调用证据

使用和 Web 客户端同一协议链验证：真实本地 OIDC Maker session、Catalog、execution-risk、短期 Run delegation、StartRun、Worker、local provider、settlement、Usage。

验证结果：

```json
{
  "workspace": "ws_local",
  "tool": "Company lookup",
  "tool_id": "company_lookup",
  "run_id": "run_e24f1b2055e6ddc20272da6035292054",
  "outcome": "succeeded",
  "charged_micro": "5000",
  "catalog_published_tools": 1
}
```

这证明本地 demo 的完整执行链，而不是外部供应商、真钱支付或生产能力认证。

## 浏览器验证

当前 Mender 工具连接未提供 Browser/IAB，因此使用仓库既有 Playwright Chromium 1.58.2 作为 fallback。实际 Maker／Checker OIDC 登录后验证：

- Console：Tools → Company lookup → Run tool → Usage；桌面与 390×844 移动视口；登录后 browser errors = 0；无水平溢出。
- Admin：Overview → Publication reviews；桌面与移动视口；browser errors = 0；无水平溢出。
- QA 截图位于 `.tmp/product-qa/`，属于本地临时验证产物，不提交仓库。

本轮没有像素级复刻 Monid；用户提供的 Monid 截图只作为信息架构参照。没有可用的本地截图 `view_image` 桥接工具，因此不能宣称完成了设计稿到渲染截图的像素级视觉签核；实际运行页面由 Playwright 功能／布局门禁和用户本机浏览共同验收。

## 工程回归

最终 `pnpm check` 通过：状态／合同／G3／S4 检查、架构规则、ESLint、TypeScript、167 项 Node 测试、全部 Go 测试、vet、Go build、Console/Admin build 均通过。Vite 仍报告两个主 bundle 大于 500 kB，这是后续性能优化项，不作为本轮产品可用性阻断条件。

历史任务台账只刷新本轮明确审阅的 Console/Admin router SHA-256 evidence reference，没有修改历史 implementation／verification／acceptance 结论。
