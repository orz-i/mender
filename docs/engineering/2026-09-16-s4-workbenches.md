# 原 S4-09 / S4-10 / S4-11 工作台与真实浏览器验证

这不是新增阶段。发布者、插件审核/发布、平台异常、支持JIT、账务/模拟支付五个页面复用原 S4 后端能力。原工作包的历史 evidence 仍按其当时范围保留，本文件补充后续界面事实，不将原来 backend COMPLETE 改写成当时已完成界面。

## 实际入口

| 应用 | 路由 | 用户流程 |
| --- | --- | --- |
| Console | `/publisher` | 发布者、插件和声明式版本草稿；服务端预检；提交独立审核；获批后发布 |
| Admin | `/release-management` | 精确插件版本审核；发布计划、cohort灰度、晋级、排空、回退和已批准应急停用 |
| Admin | `/platform-operations` | 工作区冻结、Provider隔离、事故处理、安全审计页导出 |
| Admin | `/support-approvals` | 精确审批读取；独立复核；临时支持申请、激活、撤销和只读Run |
| Admin | `/billing` | 不可变账本汇总/对账；退款与调账审批/补充分录；sandbox支付意图、回调和对账 |

操作表单只使用固定端点，不能提交actor/state/权限字段；Workspace、对象和版本在服务器重校验。写入必须CSRF和明确确认，不自动重试。审核前需先读取并选择具体审批，确认框展示实际目标、参数、金额和摘要；浏览器确认不替代maker/checker。

金额、sequence、revision均保持十进制字符串；支付模式固定sandbox。浏览器不提供回调写入或“跳转成功即到账”功能。工作区切换清空待确认操作和结果并取消旧请求。响应合同拒绝多余字段、数值精度漂移和过大响应，错误投影不回显原始服务端数据。

## 真实验证而非静态截图

显式命令 `pnpm test:s4:browser` 先构建两应用，再通过现有自有临时PostgreSQL流程运行Chromium。需要预先执行 `pnpm exec playwright install chromium`，并将 `PLAYWRIGHT_BROWSERS_PATH` 设为工作区 `.tmp/playwright-browsers`。

`backend/tests/integration/s4_browser_test.go` 使用真实数据库角色、HumanSessionService、原有业务应用服务和HTTP handlers，托管真实dist。浏览器通过UI完成草稿→预检→独立审核→发布、自审拒绝、不可变版本拒绝、冻结版本冲突、查看者拒绝、事故与审计导出、JIT申请/复核/激活/撤销、部分退款与同键重放、sandbox pending与对账，以及窄屏/确认取消/Workspace隔离。

测试身份在隔离数据库预配，使用真实服务签发短期会话；**不认证外部OIDC身份提供商**。没有mock业务HTTP或浏览器状态，没有向真实Provider/PSP发送请求。会话仅传递到本地子进程环境，不写入报告、截图或仓库；标准数据库套件不自动声称执行了浏览器测试。

Node客户端合同测试为 [commercial.test.mjs](../../frontend/packages/api-client/src/commercial.test.mjs)，真实浏览器编排为 [s4-browser-runner.mjs](../../scripts/s4-browser-runner.mjs)。输出保存到忽略的 `.tmp/s4-browser/`，正式收口记录另以实际执行回执关联版本。

## 联调发现并修复的真实缺口

平台财务/支持复核人原本可以按ID作出审批，却不能在不加入租户的前提下读取精确审批。新增对象级 GET `/api/admin/v1/workspaces/{workspace_id}/dangerous-operations/{approval_id}`；按动作区分租户发布管理或Platform Staff请求/复核权限，不增加租户全表列表权限。

JIT撤销后的PostgreSQL拒绝可能通过 `rows.Err()` 返回，旧适配器误映射为503。现在保留42501→403，用户看到权限失效而不是服务故障；支持人员仍不能取得独立复核人的撤销权限。

## 仍然不包含

外部客户实测、生产MFA、真实PSP网络和商户资质、供应商商业许可、跨浏览器全矩阵、生产部署、完全自动的财务审批均不由此验证授予。灰度是原固定cohort方案，不是百分比随机分流。发布预检不等于试调用上游。列表/导出有后端页上限；正式业务验收需确认该范围符合首发使用场景。
