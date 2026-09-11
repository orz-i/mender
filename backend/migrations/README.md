# 数据迁移

当前迁移为 `0001–0010`。`0010_provider_results.sql` 新增 append-only `execution.provider_observations`、`provider_waiting/finished` Job 状态和 provider-result deferred bundle；accepted submission 从此在同一事务释放本地 Worker lease，异步供应商处理由独立 Reconciler 收敛。`0009_supplier_runtime.sql` 新增 Supply 自有的非秘密 Deployment transport 描述；`0008_supplier_submission.sql` 建立 durable submission intent、provider request/external task 标识与 unknown/reconciling 基础；`0007_worker_leases.sql` 扩展 Job 并新增 Run Attempt、租约代次、到期索引、FORCE RLS 与 lease/attempt 一致性约束。应用既有库迁移仍由操作员显式执行，不在 API/Worker 启动时自动执行。

本轮新增 `0004_atomic_admission.sql`：commerce 预算期／额度预留和 execution 受理／blocked Job／Outbox，含 FORCE RLS 与延迟额度总和约束。0001–0003 内容不变。已有开发数据库应用新增迁移后须重新执行 `pnpm db:grant-runtime --role <查询运行角色>`，仅补充受理关联的两列读取权；不能将 admission writer 或迁移所有者混入查询 API。详情及真实故障注入结果见 [内部原子受理](../../docs/engineering/2026-09-09-atomic-admission.md)。

`0003_execution_read_indexes.sql` 归 execution 所有，为 Workspace＋创建时间＋C 排序 ID 以及状态筛选建立 keyset 索引；原 0001／0002 内容未覆盖。该子集已随隔离 PostgreSQL 套件验证；当前不承诺新旧版本滚动兼容。

已有两个顺序迁移：`0001_identity.sql` 拥有工作区／服务账号／机器 Key；`0002_execution.sql` 拥有 Run／操作者事件及 Workspace RLS。元信息在 `mender_meta`，仅操作员写入；API 启动只核验版本、摘要和受限角色，不执行迁移。

从仓库根使用 `pnpm db:migrate`，仅读取 `MENDER_ADMIN_DATABASE_URL`。代码按事务＋锁执行并校验已应用摘要，换行归一化；不能覆盖已应用 SQL 或手动修改摘要。没有破坏性自动 down migration，回退需单独评审。

`pnpm db:grant-runtime --role mender_app` 只授予已有角色必要的 SELECT、Run 特定列 UPDATE 和事件 INSERT，不创建角色／密码，不清理历史宽授权。API 拒绝 superuser、BYPASSRLS、表／schema owner、危险角色继承及过宽写权限。迁移管理用户必须和运行用户分开。

Worker 控制角色使用 `pnpm db:grant-worker --role mender_worker`。`0008` 后它获得 execution.jobs／run_attempts 的必要读写、run_admissions 中 workspace/run/deployment_revision 三列读取，以及 `execution.runs` 的只读与 state/version/updated_at 列更新、`run_events` 仅 INSERT，用于在 fencing 与数据库 bundle 约束下记录供应商提交状态。它仍不能读取 canonical arguments、机器身份、预算/价格、Connection，不能改 Run 身份/创建时间，也不能访问 Outbox 或 cancellation facts。Worker startup 会再次验证角色，不接受 owner/runtime/admission/cancellation 角色替代。完整边界见 [Worker 租约记录](../../docs/engineering/2026-09-10-worker-leases.md)和 [供应商提交协议](../../docs/engineering/2026-09-10-supplier-submission.md)。

Provider result 使用第三个独立执行角色：`pnpm db:grant-reconciler --role mender_reconciler`。该角色可读取 Run/Job/Attempt/result observation 控制事实、追加 `provider_observations`/RunEvent，并只更新 Run state/version/updated_at 与 Job state/blocked_reason/updated_at/stopped_at；它不能读取 `run_admissions`、Identity、Commerce、Connections、Supply、Outbox，也不能修改 Attempt 或既有 observation。完整语义和真实 PostgreSQL 证据见 [Provider Result Lifecycle](../../docs/engineering/2026-09-10-provider-result-lifecycle.md)。

供应商执行材料使用**另一独立角色**：`pnpm db:grant-executor --role mender_executor`。该角色仅可读取 `execution.run_admissions` 的 workspace/run/subject/connection/tool_version/deployment_revision/canonical_arguments、Connections 当前授权与 `credential_version_ref`、`supply.deployments` 以及迁移摘要；它不能读取 identity、commerce、catalog、Worker Job/Attempt/Run/Event/Outbox，也没有任何业务表写权限。`credential_version_ref` 只是 opaque secret-store key，不是 secret 本身。完整边界见 [Supplier Runtime Broker](../../docs/engineering/2026-09-10-supplier-runtime-broker.md)。

已有环境应用 `0008` 后必须重新执行 `pnpm db:grant-worker --role <原 worker role>`，否则 WorkerRole 启动校验会因缺少 submission 状态所需的窄权限而失败关闭。该命令不授予任何 supplier credential、commerce、connections 或 catalog 访问。

应用 `0009` 时应新建独立 executor login role 并运行 `pnpm db:grant-executor --role <executor role>`；不要把 API runtime、admission、cancellation、worker 或迁移 owner 复用为 executor role。现有 Worker/API 角色无需获得 Supply schema USAGE，启动校验会继续拒绝越权配置。

应用 `0010` 时应新建独立 reconciler login role 并运行 `pnpm db:grant-reconciler --role <reconciler role>`。既有 Worker 不需要新增 provider-result 权限；相反，Worker/API/admission/cancellation/executor 的启动校验都要求它们无法访问 `provider_observations`。`0010` 会重建 submission bundle，使 accepted Attempt 与 `Job=provider_waiting` 配对并清空 Worker lease，因此旧的“已 accepted 仍靠 lease expiry 进入 reconciliation”行为被明确废止。

已有环境应用 `0007` 后还应重新执行 `pnpm db:grant-admission --role <原 admission role>`。该命令会主动撤销旧的 `execution.jobs` 整表 INSERT，再只授予 StartRun 所需的 workspace/run/state/blocked_reason/available_at/created_at/updated_at 列，防止旧 admission role 因新列出现而获得 priority、lease、fencing 或 attempt 配置写入能力。

启用“已 activation 但从未 lease 的 queued Job”协调取消后，也应重新执行 `pnpm db:grant-cancellation --role <原 cancellation role>`。该授权仅增加 `run_attempts` 的 workspace/run/attempt_no 三列读取用于证明 Attempt 不存在，并允许同步 Job `updated_at`；任何已有 Attempt 的 Job 都不会走即时额度释放路径。

真实数据库验证入口为 `pnpm test:integration`，缺少配置明确失败；本机自有临时资源入口为 `pnpm test:integration:docker`。`0010` 已通过隔离 PostgreSQL 套件，包括 accepted lease release、Reconciler 最小权限、pending/terminal observation bundle、幂等 replay、乱序拒绝、第二终态拒绝和旧取消 fixture 兼容。
