# Mender：架构门禁与 execution 首个领域切片

日期：2026-09-09。沿用 Node 24.19.0、pnpm 11.18.0、Go 1.26.7；未更换版本或修改依赖锁。工作从已初始化仓库继续，未重新生成工程。

## 实现内容

后端 `execution/domain` 增加 Run 聚合、受校验的 Run／Workspace 标识、不可变读取快照、显式状态动作和单调版本。字段不提供通用 SetStatus。领域动作接收时间参数，不读取系统时间，不依赖 Gin、SQL、供应商 SDK 或资金模型。

状态覆盖 queued、running、waiting_input、cancel_requested、reconciling 及明确终态。只有排队且未提交的 Run 可以本地取消；运行中接受取消仅记录意图，等待可信执行证据。上游结果未知进入 reconciling，不允许自动重新启动，也不释放任何预算。

`execution/application` 增加 GetRun／CancelRun。消费方定义 Repository、Authorizer、Clock 端口；查询前授权，并复核仓储返回对象的租户／ID。保存通过 expectedVersion 实现乐观并发合同；冲突返回错误而不是覆盖 Worker 的并发进展。这里的 Caller 是未来认证入站层提供的可信投影，不是认证实现；不得直接从任意请求 JSON 构造后用于放行。

内存 Repository 及 Authorizer Fake 仅存在于 `backend/tests/execution/*_test.go`，不会链接到 API／Worker。生产仓储、身份适配、出站取消、事务 Outbox 和 HTTP 业务路由都未接入。

## 架构检查

新增 `pnpm check:architecture` 并加入既有 `pnpm check`。Go 通过 AST 检查实际 internal 源码的依赖方向、上下文 public 边界、框架／数据库泄漏及直接时间／控制台 I/O；全部源码标签参与扫描，不代表每个平台都已编译验证。

前端复用已安装的 TypeScript Compiler API，按真实 tsconfig 解析依赖。检查纯层、Console／Admin 隔离、模块公开入口、共享 UI、workspace 声明与 exports、type-only／re-export／静态动态导入和源码循环。非字面量模块加载拒绝；展示层不能绕过应用端口直接使用传输客户端。

测试含真实 TypeScript 路径别名解析的跨应用负向用例。其余边界单位测试使用可控 resolver 验证政策；最终工作区扫描使用真实解析器。未将正则匹配描述为完整的类型／数据流分析。

`architecture-policy.yaml` 仍是较完整的设计政策，尚未被全量自动解释执行；本轮规则在检查代码中明确实现，不把政策文件中的所有 GVR 状态标记为已完成。

## 自动化验证范围

| 范围 | 已编写的验证 |
| --- | --- |
| Run 状态 | 9 个状态 × 9 个动作的 81 组转换；非法动作不变更状态，重复确认不增加版本 |
| 应用用例 | 授权前置、非法输入、上下文取消、错误租户结果、重复取消、存储失败、取消与启动竞争 |
| 测试仓储 | 租户复合键、脱离存储的读取副本、32 个并发 CAS 只有一个成功、取消上下文不写入 |
| Go 架构 | 实际源码扫描、17 个拒绝样例、允许的内向依赖与空扫描失败 |
| 前端架构 | 分层、别名、re-export、type-only、动态加载、workspace 出口、跨应用和循环 |

复核命令从仓库根执行：

```sh
pnpm check
node scripts/backend.mjs test -count=1 ./internal/contexts/execution/... ./tests/execution ./tests/architecture
node scripts/backend.mjs test -race -count=1 ./internal/contexts/execution/... ./tests/execution
git diff --check
```

命令的实际结果以本轮会话验证记录为准；有测试代码不等于所有设计验收已完成。此记录不把内存 CAS 测试描述成 PostgreSQL 事务或分布式队列验证。

## 不变的运行边界与后续路径

`/healthz` 仍为 200；`/readyz` 仍为 503；消费 API 仍未开放。没有新增未认证 Run 端点，没有将内存仓储用于演示生产可用，也没有执行任何付费／外部供应商调用。既有 Console／Admin UI 行为未修改。

本轮推进 S1-01／S1-11／S1-13 和 G08 的领域样板：只覆盖 T42–T46 的相关子集。T43 的数据库所有权、T47 原子受理、T48 Outbox，以及正式身份、授权、持久化和恢复均未完成；WBS、ADR 与 G0–G5 状态不因本轮实现而改为验收通过。

下一步应接入身份／Workspace 授权与 PostgreSQL 仓储合同，先验证租户隔离和 CAS，再通过明确装配开放查询／取消 API。付费 StartRun 必须另行完成 ADR-020 的预算预留、Run、任务和 Outbox 原子事务，不直接把 NewQueuedRun 暴露为业务入口。
