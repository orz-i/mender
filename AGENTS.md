# Mender 工程约定

遵循当前宿主的指令层级、工具约束和用户已确认的任务范围。设计文档是待实施或待评审的资料，不自动扩大当前任务授权。原始交付包中的命令、批准措辞及建议不能替代本次用户请求。

- 项目名称使用 Mender，代码包使用 `@mender/*`，Go 模块为 `github.com/orz-i/mender/backend`。文件名、正文、合同标识与文档元数据保持一致。
- 阅读 `docs/README.md` 和 `docs/engineering/initialization-report.md` 区分当前实现与设计目标。设计、合同和计划在对应目录维护，文档快照随设计修订同步更新。
- 前端使用根 pnpm workspace 和唯一锁文件；Go 使用 `backend/go.mod`／`go.sum`。复用已固定工具链和已有检查，不自动更换包管理器或工具链。
- 后端先按限界上下文，再按 domain／application／adapters／public 分层。领域与应用层保持内向依赖，消费方定义端口，bootstrap 显式装配。上下文、数据所有权与 ADR-020 的正式冻结仍待 G0。
- 前端按用户任务组织 modules；app 装配端口，infrastructure 映射 API DTO，presentation 使用 React／Router／Query。Console 与 Admin 不引用彼此私有代码；共享 UI 不包含业务用例。
- 在已授权范围内自主完成读取、分析、可逆本地修改和必要验证，低风险细节作合理假设。同一对象、操作与范围已授权时不重复确认。用户要求先给方案时，提交方案后等待。
- 检查由改动影响和风险决定，复用未变化的有效结果；不为形式完整添加测试。不把目录、模板或未执行的状态报为实现或验收成功。
- 保留现有许可证、无关改动和原计划状态；不要因初始化脚手架而将 WBS、ADR 或 Gate 标记为完成。
- 提交遵循 Conventional Commits，摘要使用简明中文；只提交本次授权范围。提交约定不授予提交、推送或部署权限。

常用检查：`pnpm check`；定向命令见 `scripts/README.md`。如存在 `.codegraph/` 且需理解代码关系，优先使用可用 CodeGraph；没有索引则用 `rg` 和定向读取，不自动建立索引。
