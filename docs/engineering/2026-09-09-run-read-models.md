# Mender：Run 列表、有限事件时间线与隔离数据库测试入口

后续进展：本文件保留该阶段的历史结果；本机 Docker 已由用户启动，既有真实套件及扩展验证已在下一轮执行，最新状态见 [原子受理记录](2026-09-09-atomic-admission.md)。不要将下文历史环境阻塞解释为当前仍不可用。

日期：2026-09-09。本轮在已实现的机器身份、Workspace 授权、Run 状态机及 PostgreSQL 仓储上继续推进。保留 pnpm／Node／Go 固定版本，不新增依赖，不修改前端页面、不接入假登录。

## 实现边界

新增两个只读用例、独立的 ReadRepository 查询端口、签名游标端口、PostgreSQL keyset 查询与受保护 HTTP 适配器。它们用于后续 Run Explorer，不代表已经有可视化运行页面、StartRun、真实 Worker、预算或费用结算。

| 层次 | 代码位置与职责 |
| --- | --- |
| 应用用例 | execution/application/query.go：授权、游标绑定、边界检查和分页结果 |
| 消费方端口 | application/ports/query.go：只读仓储、投影和 CursorCodec |
| 适配器 | outbound/postgres/query.go、outbound/cursor/codec.go；SQL 与 HMAC 留在外层 |
| 身份防腐层 | ListRuns／ReadRunEvents 显式映射已有 Workspace 级 run:read scope |
| 入站层 | inbound/httpapi/query.go；与原详情／取消共用认证入口 |
| 装配根 | bootstrap/persistence.go：独立开关、密钥校验、只读用例装配 |
| 索引迁移 | 0003_execution_read_indexes.sql；不修改已存在的 0001／0002 内容 |

主业务读取仍是先认证、校验 Workspace、应用层重新授权，再查询。有效签名游标不是授权凭据；Key 撤销、scope 撤回或 Workspace 禁用不会因为客户端持有游标而绕过。

## HTTP 合同

窄合同见 [run-read.openapi.yaml](../../contracts/run-read.openapi.yaml)。仅支持下列明确查询参数，拒绝未知、重复、大小写绕行、空值及错误编码，不默默忽略 offset 或客户端身份字段。

```http
GET /api/v1/workspaces/{workspace_id}/runs?limit=20&state=queued
GET /api/v1/workspaces/{workspace_id}/runs?limit=20&state=queued&cursor=<next_cursor>
GET /api/v1/workspaces/{workspace_id}/runs/{run_id}/events?limit=20
```

limit 默认 20、范围 1–100；cursor 最大 2048 字符；state 为可选的单个明确 Run 状态。事件接口不接受 state。响应 data 始终是数组，next_cursor 为 null 表示此次遍历结束；不返回昂贵的总数或其他 Workspace 的统计。

列表按 created_at DESC、id 的 PostgreSQL C 排序 DESC 使用复合 keyset，不使用偏移量分页。ID 是稳定的同时间戳排序补充；数据库索引与应用检查使用一致的字节排序。只查询 limit+1 行来判断是否存在下一页。

这是实时读取，不是跨 HTTP 请求保留的 MVCC 快照。新插入且排在已读区间之前的 Run 不会挤动后续页；回填历史数据或状态过滤期间发生状态变化，可能改变尚未读取的集合。不能宣称在持续变更中一定看见某时刻的完整列表。

## 事件水位与敏感字段

事件第一页读取 Run 当前 revision 作为 through_version；后续页按 version ASC 读取，不超过第一次水位。遍历期间新产生的更高 revision 不混入本次分页；重新从第一页查询才能看见更新。through_version 和每个 version 都通过十进制字符串输出，不让 JavaScript 数值精度丢失。

已存在但还没有状态事件的 Run 返回空数组和已知水位；不存在或不属于允许 Workspace 的 Run 不伪装成空时间线。此处暴露的是 execution.run_events 中的状态记录，不是完整审计平台、事件溯源、SSE、MCP 恢复流或已实现 Outbox。

时间线返回状态、时间、主体 ID 和已记录的取消／变更原因，不返回 credential_id、凭据摘要、原始 Key、计费状态或不存在的追踪数据。原因可能包含业务内容，因此需要现有 run:read 权限，后续细粒度对象分享须扩展统一授权端口，不能仅过滤页面。

## 签名与配置

游标采用 HMAC-SHA256，绑定 Workspace、主体、机器凭据 ID、查询类型、Run、state、limit、继续位置、事件水位及到期时间。签名只保证完整性，不加密游标内容；其中不写入凭据、原始用户参数或其他秘密。

期限为 15 分钟，翻页不延长原期限。改变过滤条件／limit、切换凭据／Workspace、跨接口使用、篡改或过期均返回 INVALID_CURSOR，要求重新遍历。服务端签名密钥轮换会使旧游标失效，目前没有双密钥过渡期。

原有查询／取消开关保持原义；列表／事件需要额外显式打开：

```text
MENDER_RUN_API_ENABLED=true
MENDER_RUN_READ_API_ENABLED=true
MENDER_DATABASE_URL=<受限运行账号的安全注入连接>
MENDER_CURSOR_SIGNING_KEY=<32 字节密码学随机数的无填充 base64url，43 字符>
```

所有副本使用同一个受保护的签名密钥，不能使用每次启动变化的随机值或数据库密码充当签名密钥。配置文件示例不含真实值，不要把秘密填入仓库或聊天。列表开关与认证开关不一致、密钥缺失／格式错误／全零时失败关闭。

默认两个业务开关都未启用；不会为演示自动建库、填入 Run 或回退内存存储。API 仍不在启动时执行 DDL。新增索引需要操作员先运行迁移，旧 API 的严格迁移集检查可能拒绝升级后的数据库，因此本阶段不是滚动升级兼容方案，应停机受控更新并重新验证。

## 可重复的隔离 PostgreSQL 验证

新增命令从仓库根执行：

```sh
pnpm test:integration:docker --check
pnpm test:integration:docker
pnpm test:integration:docker --pull
```

--check 只验证本地 Docker endpoint 与守护进程，不创建数据库；默认运行只使用已有 postgres:18.6 镜像；--pull 才允许缺失时从官方镜像仓库拉取。版本与已有 CI 基线一致，并通过脚本测试检查漂移。镜像 tag 不是不可变 digest，正式供应链锁定仍需单独评审。

编排器只允许本地 Unix socket／Windows named pipe，不使用 TCP／SSH 远程 Docker endpoint。要求 Engine 28+ 作为回环端口发布的安全基线；随机主机端口仅绑定 127.0.0.1，不占用固定 5432。

测试为本次调用生成随机容器名、所有权 label 和随机数据库密码；密码只通过子进程环境传递，不出现在 Docker 命令参数或连接日志里。容器数据使用有大小限制的临时文件系统，不挂载仓库、宿主数据目录或外部数据库卷，也不需要持久配置管理数据库密码。

真实套件在该容器中创建自己命名的临时数据库与角色。成功、测试失败、就绪超时和取消后都尝试检查本次 label，再删除精确名称的自有容器；绝不按前缀批量 prune 或停止用户的其他容器。清理失败或归属不符仍返回失败，不覆盖原始测试失败。进程被强制杀死、机器断电等无法运行清理时，需人工核对记录中的精确名称与 label，不应执行全局清理。

该工具不会启动 Docker Desktop 或修改宿主系统服务。没有 Docker 的环境仍可使用原 pnpm test:integration，但必须由安全环境配置专用回环数据库及 MENDER_TEST_ALLOW_CREATE_DATABASE=true，禁止指向生产服务。

## 测试与真实限制

新增应用／HTTP 测试覆盖相同时间戳的稳定排序、下一页绑定、过期与篡改、跨主体／凭据／Workspace／接口重放、撤销前置、空数组、错误存储投影、事件水位及敏感字段。签名器和配置均有负向测试。

扩展真实 PostgreSQL 套件，覆盖 C 排序索引、分页间插入、RLS、连接池租户上下文清理、真实状态事件水位、撤销以及已有迁移／CAS／回滚路径。这些用例只有连到真实 PostgreSQL 执行成功，才算持久化验证。

本轮实际 --check 返回 exit 1：本地 Docker 守护进程仍不可用。原 pnpm test:integration 也返回 exit 1：专用数据库条件未配置。套件已编译，但 SQL 测试体没有执行；不把 Fake 编排测试或 Gin 测试仓储称作 PostgreSQL 验收通过，也不启动宿主服务来绕过这一条件。

| 已执行检查 | 本机结果 |
| --- | --- |
| 最终 pnpm check | exit 0：合同、架构、lint、类型、Go 测试/vet/build 与 Console／Admin 构建通过；首轮发现的 finally 抛错 lint 问题已修复后复验 |
| Node 测试 | 20 项通过，包括 7 项隔离编排／安全／清理路径测试；不代表真的启动过 Docker |
| 最终定向 go test -race -count=1 | execution／HTTP／identity／architecture／bootstrap 五个包通过；无缓存执行 |
| Docker 预检 | exit 1：daemon 不可用；未创建容器或下载镜像 |
| PostgreSQL 集成入口 | exit 1：缺少显式专用测试连接；SQL 测试体未运行 |

这些是开发验证记录，不是 G0–G5 的正式验收签署。真实 SQL、RLS 和事务验证仍是下一次执行前不能跳过的条件。

复核命令：

```sh
pnpm check
node scripts/backend.mjs test -count=1 ./tests/execution ./tests/httpapi ./tests/identity ./tests/architecture ./internal/bootstrap
node scripts/backend.mjs test -race -count=1 ./tests/execution ./tests/httpapi ./tests/identity
pnpm test:integration:docker --check
git diff --check
```

实现推进不修改 WBS、ADR 或 G0–G5 的正式验收状态。下一步优先启动并验证本机 Docker 或准备专用测试 PostgreSQL，完成真实套件后再推进执行队列和预算受理；不从列表 API 旁路创建未预留的付费 Run。

## 一手参考

- PostgreSQL LIMIT／排序：https://www.postgresql.org/docs/current/queries-limit.html
- PostgreSQL RLS：https://www.postgresql.org/docs/current/ddl-rowsecurity.html
- Docker run：https://docs.docker.com/reference/cli/docker/container/run/
- Docker 回环端口发布：https://docs.docker.com/engine/network/port-publishing/

参考用于实现与限制说明，不代替本项目真实数据库测试。
