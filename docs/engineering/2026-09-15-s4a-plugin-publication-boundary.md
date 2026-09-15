# S4-A Plugin / Publisher Manifest 与 Publication State Model

日期：2026-09-15  
机器证据：[s4a-plugin-publication-evidence.json](s4a-plugin-publication-evidence.json)

## 结论

**S4-A：COMPLETE**。

本工作包完成的是受控 Plugin / Publisher 发布模型，不是公共插件代码执行平台。`PluginManifest` 只能声明 Mender 已支持的 `api_tool`、`mcp_tool` 与 reviewed `agent` capability 引用；Manifest 不接受 executable artifact、script、endpoint、secret、权限代码或任意 policy code。

## 已完成范围

- Workspace-scoped `Publisher → Plugin → PluginVersion` 所有权；
- versioned Plugin Manifest 与服务端 SHA-256 digest；
- `draft → submitted → approved → published` 主发布链；
- `deprecated / disabled` 作为显式持久化状态，后续 Release Governance 再定义运行期语义；
- draft 内容更新自动推进 `revision`；
- submitted/approved/published Manifest material 不可修改，已发布版本不可删除；
- capability preflight 在 submit 与最终 publish 两处执行，避免 stale approval 绕过 capability 状态变化；
- maker/checker：请求人与 reviewer 不能是同一用户；审批绑定 `plugin_id + version + target_revision`；
- reject / approval expiry 不伪造发布成功，并安全返回 draft；
- append-only publication audit；
- Publisher manager 与 Governance reviewer 使用不同 least-privilege PostgreSQL role；
- Publisher、Plugin、PluginVersion、approval 与 audit 均保持 Workspace FORCE RLS；
- Console Publisher API 支持 draft 管理、preflight、submit、publish；
- Admin API 支持 review queue、approve、reject；
- 浏览器 projection 中 revision 按 decimal string 输出，不暴露 Publisher owner user id。

真实 PostgreSQL 集成测试覆盖 direct-SQL malformed Manifest、跨 Workspace RLS、Publisher owner 校验、自审拒绝、reject→draft、exact revision、approve、发布前 capability 二次检查、published immutability、audit convergence 与两类 restricted role 的双向越权负例。

## 明确边界

S4-A 没有引入任意用户代码执行、脚本运行、动态 policy code、A2A 或 multi-turn Agent。Manifest 是平台已有受控 capability 的声明性组合，不是新的 runtime artifact。

本工作包也**没有**声明以下能力已经完成：canary、drain、rollback、emergency disable、历史 Run 的 release pinning、Publisher/Admin 前端 Workbench、商业账务、支付适配器、CLI / Skill distribution，以及 production deploy。这些范围继续 deferred；其中下一工作包是 **S4-B Release Governance**。

现有 quota / usage settlement 仍不能解释为 payment / revenue accounting，Plugin publication audit 也不是支付或结算账本。

## 运行与权限边界

`MENDER_CONSOLE_PUBLISHER_ENABLED=true` 要求 Human OIDC 与独立 publisher-manager 数据库身份；`MENDER_ADMIN_PLUGIN_REVIEW_ENABLED=true` 进一步要求独立 governance-reviewer 身份。启动与 readiness 都校验迁移和实际 grants，配置字符串本身不能提升数据库权限。

Publisher manager 可以管理自己拥有的 draft，并通过受控 wrapper submit / consume approval；它拿不到 raw lifecycle 函数和 reviewer authority。Governance reviewer 可以读取 review facts 并 approve / reject，但拿不到 Publisher publish authority。最终 publish 仍由 Publisher owner 发起，并再次以数据库真源校验 capability。

