# INIT-001 · 项目更名为 Mender

日期：2026-09-08。状态：**项目名称由本次用户请求确定；下述工程映射用于初始化**。本编号不占用原设计 ADR-001–ADR-020。

## 命名映射

| 对象 | 当前规则 |
| --- | --- |
| 产品名称、README、页面标题 | Mender |
| 仓库目录与根包 | `mender` |
| 前端包 | `@mender/console`、`@mender/admin`、`@mender/ui`、`@mender/api-client`、`@mender/config` |
| Go 模块 | `github.com/orz-i/mender/backend`，根据当前 Git remote 路径设置 |
| 本地环境变量 | `MENDER_*` |
| 当前可编辑设计文档 | 文件名与正文大写 MCPX 替换为 Mender；工作区示例 `@mcpx/` 替换为 `@mender/` |
| 任务、需求、测试、ADR、GVR 编号 | 保留原编号与状态 |

## 有意保留的历史名称

原始交付归档保留文件名与字节。原始 DOCX、PDF、XLSX 没有被改名后冒充重新导出的 Mender 制品。

合同中的 `mcpx.io/plugin/v1alpha1`、`application/vnd.mcpx.declarative+json` 和示例 Schema URL 的 `/mcpx/` 暂时保留。这些是协议／媒体类型标识，不是 UI 品牌文本。更名未要求发布新的协议版本，且设计包未包含兼容性迁移证据。新的协议命名空间、迁移策略和版本在 G0 合同冻结时一起决定；保留不代表这些示例已经对外发布。

不将命名空间直接替换为某个未经确认的真实域名；不改动官方 MCP 协议名称、第三方名称、示例数据语义或原制品摘要。

## 工作副本来源

- `docs/design/` 来源于原包 `sources/`，增加当前状态提示；原始历史事实以 2026-09-04 为时间边界。
- `docs/diagrams/` 来源于原包 `diagrams/`，配图内容未改动。
- `docs/planning/project-data.json` 来源于原包 JSON，增加项目名称与快照说明，保留任务状态。
- `contracts/` 修改展示标题，保留 wire 标识和样例语义；新增 Node 校验入口。
- `docs/engineering/` 保存设计政策和模板；真实工具链在仓库根，真实 CI 在 `.github/workflows/`。

后续若需要更新对外 Office／PDF 交付，应以当前工作文档为源创建新发布目录并重新校验，不修改历史目录。
