# Mender：PostgreSQL 实证与内部原子受理

日期：2026-09-09。沿用 Node 24.19.0、pnpm 11.18.0、Go 1.26.7、pgx v5.11.0 和已有 PostgreSQL 18.6 隔离测试镜像；本轮不修改依赖锁或工具链。

## 环境与进展

用户已启动本机 Docker 并提供真实数据库套件通过结果。本轮首先在当前工作树重新运行 `pnpm test:integration:docker`，得到 exit 0、真实测试通过及自有容器已清理。之前报告中的 Docker／数据库阻塞属于历史状态，不再是当前阻塞。

本轮从只读 Run 能力推进到内部原子受理：在一个数据库事务中写入预算额度预留、Run、不可变受理快照、blocked Job 和待投递 Outbox。没有将公共 StartRun、真实工具执行或付费服务提前开放。

## 实现与领域边界

| 模块 | 已实现内容 | 明确不包含 |
| --- | --- | --- |
| commerce | 精确整数微单位、Allowance 聚合、预算期行锁与预留 | 支付余额、收入、复式账本、退款、结算或自动释放 |
| execution | Run 创建、受理快照、Job 与 Outbox，幂等回执 | 真实 Worker、租约领取、上游调用、消息分发 |
| admission/application | 授权、解析端口、请求规范化、幂等、跨域步骤顺序 | Gin、pgx、SQL、供应商 SDK、真实 Catalog 实现 |
| admission 出站适配 | public 防腐层与专用 UnitOfWork | 跨域直接写对方表 |
| bootstrap | 同一个 pgx.Tx 装配 commerce／execution 的各自服务 | 给运行中的 API 自动安装假 Authorizer 或 Resolver |

`BuildAdmission` 要求显式提供 Authorizer 和 Resolver，并检查独立 admission writer 的迁移／权限。生产 Catalog、Toolset、Connection、价格合同及创建权限适配尚未接入；测试中的这两个端口为可控 fixture，但数据库与事务不是 Mock。API 与 Worker 都没有调用此内部装配入口。

本轮额度的币种和微单位用于工程预算控制，不能据此称为真实钱包或会计账本。没有创建实际收费余额、支付订单或持久演示用户数据。

## 一次受理的行为

1. 校验调用主体、明确工具／工具集版本、连接、预算期、币种、用户上限和幂等键。
2. 每次尝试均通过当前授权；解析经认可、不可变且有可靠成本上限的执行计划。缺少端口或计划无效时拒绝。
3. 在适配器中规范化 JSON 对象和生成请求摘要。金额使用十进制整数字符串；参数数字保留原始数字词法，不转成 float64。
4. UnitOfWork 开启事务并设置事务级 Workspace；先取得幂等键锁，再取得预算期行锁。
5. 相同 Workspace／subject／幂等键且摘要相同，返回原受理回执，不再占额度；同键不同摘要冲突。当前 Key 撤销不能被旧幂等键绕过；同主体换 Key 不重复创建。
6. 检查预算可用额、有效期及币种，预留后顺序创建 Run、受理快照、Job、Outbox；全部提交后才返回成功。
7. 任何中间写入错误或计划过期回滚全部写入；提交结果不明确时返回明确的未确认错误，要求使用同一幂等键重试，不返回假成功。

JSON 规范化拒绝重复字段（包括转义后同名）、非对象、尾随 JSON、非法 UTF-8／未配对 UTF-16 转义、NUL、过深结构及超大输入。对象字段顺序和无意义空白不改变摘要；数字 `1`、`1.0`、`1e0` 保留不同词法并可能被视为不同请求。这是本项目固定版本的规范化规则，不宣称完整实现 RFC 8785。

幂等回执是“原受理结果”，不是最新 Run 状态；最新状态仍通过已有查询接口读取。重放还需要当前授权与可解析的计划，不承诺在权限撤销或工具合同不可用后依然返回旧结果。

## 数据库保障

新增 `0004_atomic_admission.sql`，不修改此前 0001–0003 的内容。commerce 拥有预算期与预留；execution 拥有受理、Job、Outbox。新表均启用 ENABLE＋FORCE RLS，所有适配 SQL 同时绑定 Workspace。

预算期以 `SELECT … FOR UPDATE` 串行保护额度。比较使用减法上界，避免大整数加法溢出；取得锁后重新检查数据库时间，计划在等待和写入后再次复核。预算上限、已消费和当前预留的约束在数据库与领域两层校验。

延迟约束验证 `reserved_micro = SUM(held reservations)`，不允许只改汇总额度而没有对应预留。即使在提交前改变 RLS Workspace，也不会把看不见预算行当作验证成功。该约束维护额度占用，不是资金分录守恒或会计账本证明。

专用 admission writer 可以读预算和受理回执、更新预算预留／revision，并插入自己范围内的预留／Run／快照／Job／Outbox；不能调整预算上限、改身份、修改 Run 状态或删除事实。查询／取消 API 角色不能写这些新表，只增加受理关联的两列只读权限以识别受控任务。

首次真实扩展测试发现：无 commerce schema USAGE 的查询角色无法用字符串表名完成权限元数据检查。已改为按 catalog OID 检查，并保持原有禁止访问 commerce 的角色边界，没有通过放宽权限修复测试。

## 执行与取消保护

Job 当前只能为 `blocked / executor_not_configured`，没有可被执行器领取的 ready 状态。Outbox 只是事务性持久化记录，仍为 pending；未实现投递器、消费去重、重投或外部消息调用。事件只包含必要 ID 和状态，不序列化参数、Key、连接秘密或整个聚合。

为避免旧取消接口将有预留的 Run 单独改成 canceled，现有 POST cancel 对受理关联任务返回 `409 ADMISSION_CANCEL_UNAVAILABLE`。它不会释放额度、修改 Job 或伪称已停止。旧的无预留 Run 查询／取消语义继续通过回归测试。

下一步必须实现协调取消：Run 状态、Job 禁止执行、预留释放以及事件在同一个事务变化。公开创建入口还需真实授权／Catalog／连接／计价适配；实际执行另需 Worker 租约、fencing、幂等、上游结果确认与结算。不要通过直接改表启用当前 blocked Job。

## 验证方式

```sh
pnpm test:integration:docker
node scripts/backend.mjs test -race -count=1 ./tests/admission ./tests/execution ./tests/httpapi ./tests/architecture ./internal/contexts/commerce/... ./internal/bootstrap
pnpm check
git diff --check
```

真实 PostgreSQL 套件新增：12 个同键并发只创建一组受理记录、16 个不同键竞争紧额度只有一个成功、Key 轮换重放、跨租户相同键独立、六个持久写入点逐个失败完整回滚、失败后同键可重试、延迟额度总和约束、RLS 切换不能绕过约束、过期与币种不匹配、运行角色负向权限，以及真实 HTTP 对受理任务的取消保护。

故障注入仅在套件自己创建的数据库中临时增加约束，测试结束清理自有数据库、角色和容器。不操作生产数据库、不启动宿主服务、不保留测试账户或余额。测试角色与计划 fixture 不代表人类登录或生产 Catalog 已实现。

### 本轮实际验证结果

| 验证 | 2026-09-09 本机结果 |
| --- | --- |
| 修改前隔离 PostgreSQL 基线 | exit 0；既有真实迁移／RLS／CAS／查询套件通过，自有容器清理 |
| 最终扩展 PostgreSQL 套件 | exit 0；包含原子受理、并发额度与幂等、六写点回滚、延迟约束、RLS切换、角色分离和HTTP取消保护；没有 Skip；容器清理 |
| 最终 `pnpm check` | exit 0；工具链、workspace、文档、合同、Go/TS架构、lint、类型、测试、vet、后端及Console/Admin构建 |
| 非缓存 Go race | exit 0；tests/admission、execution、httpapi、architecture、commerce领域和bootstrap |
| Node 测试 | 20 项通过、0失败、0跳过；其中容器编排控制流测试仍为Fake，不替代上述真实SQL |

真实套件的初次扩展运行暴露权限元数据查询问题，修复后同一验证入口重新通过；未把失败改成预期成功，也未通过放宽数据库角色绕过。`pnpm check` 不包括真实数据库套件，后者已另外实际运行。

以上记录不将内部测试策略／解析fixture变成生产权限或Catalog实现。设计文档、WBS、ADR-020 及 Gate 不因为实现和测试而自动标为正式批准。

## 工程与迁移注意

Go 架构检查扩展到 `internal/processes/admission`：流程 application 不直接依赖 SQL、他域 public 或具体适配器；出站防腐层只消费他域 public；bootstrap 才能装配具体实现。增加故意违反上述规则的样例。

API 默认仍关闭业务子集，内部 admission 没有环境开关或公开路由。已有开发库应用 0004 后需要用原操作员命令重新授予查询角色必要的受理关联只读列；启动继续严格验证迁移集合。不要重写已应用迁移，也不要把管理角色作为应用连接。`GrantAdmission` 为内部受控能力，本轮不增加操作员 CLI 或生产配置示例。

## 参考资料

- pgx 固定版本事务接口：https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0
- PostgreSQL 显式锁：https://www.postgresql.org/docs/current/explicit-locking.html

以上仅支撑实现语义；本轮真实 SQL、权限和并发是否通过，以实际套件结果为准。
