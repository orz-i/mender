# Go 架构边界检查

`go test ./tests/architecture` 使用 `go/parser` 扫描 `backend/internal` 下的全部 Go 文件，包含生成源码、测试源码和当前主机未启用的构建标签文件；不执行被扫描文件。扫描为空或解析失败会报错。

检查内层依赖白名单、domain／application／public 的源码方向、跨上下文仅从出站适配器访问 public、入站不得直接依赖出站仓储、platform 不包含业务依赖，以及直接的环境时间／控制台 I/O 引用。测试包含 17 个拒绝样例和合法依赖样例，不只检查目录存在。

前端对应 `scripts/check-architecture.mjs`。聚合命令为根目录 `pnpm check:architecture`，已接入 `pnpm check`，因此现有 CI 同时执行这些规则。

边界：本实现使用 AST 与导入路径规则，不是完整的 Go 类型／数据流分析。全部构建标签文件参与源码扫描，但不表示已在所有平台编译运行；包循环由 Go 构建检查。SQL 表所有权、间接副作用、架构例外的审批／到期、真实数据库与分布式事务仍需后续验证，不能将 T42／T43 整项标为完成。
