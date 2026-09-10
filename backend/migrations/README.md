# 数据迁移

当前迁移为 `0001–0008`。`0008_supplier_submission.sql` 为 execution 增加 durable submission intent、provider request/external task 标识、unknown/reconciling 状态及延迟 submission bundle 证明；`0007_worker_leases.sql` 扩展 Job 并新增 Run Attempt、租约代次、到期索引、FORCE RLS 与 lease/attempt 一致性约束；`0006_start_run_plan.sql` 提供 StartRun 的本地计划事实；`0005_coordinated_cancellation.sql` 支持可证明从未提交供应商的受理任务取消与原周期预留释放。应用既有库迁移仍由操作员显式执行，不在 API/Worker 启动时自动执行。

本轮新增 `0004_atomic_admission.sql`：commerce 预算期／额度预留和 execution 受理／blocked Job／Outbox，含 FORCE RLS 与延迟额度总和约束。0001–0003 内容不变。已有开发数据库应用新增迁移后须重新执行 `pnpm db:grant-runtime --role <查询运行角色>`，仅补充受理关联的两列读取权；不能将 admission writer 或迁移所有者混入查询 API。详情及真实故障注入结果见 [内部原子受理](../../docs/engineering/2026-09-09-atomic-admission.md)。

`0003_execution_read_indexes.sql` 归 execution 所有，为 Workspace＋创建时间＋C 排序 ID 以及状态筛选建立 keyset 索引；原 0001／0002 内容未覆盖。该子集已随隔离 PostgreSQL 套件验证；当前不承诺新旧版本滚动兼容。

已有两个顺序迁移：`0001_identity.sql` 拥有工作区／服务账号／机器 Key；`0002_execution.sql` 拥有 Run／操作者事件及 Workspace RLS。元信息在 `mender_meta`，仅操作员写入；API 启动只核验版本、摘要和受限角色，不执行迁移。

从仓库根使用 `pnpm db:migrate`，仅读取 `MENDER_ADMIN_DATABASE_URL`。代码按事务＋锁执行并校验已应用摘要，换行归一化；不能覆盖已应用 SQL 或手动修改摘要。没有破坏性自动 down migration，回退需单独评审。

`pnpm db:grant-runtime --role mender_app` 只授予已有角色必要的 SELECT、Run 特定列 UPDATE 和事件 INSERT，不创建角色／密码，不清理历史宽授权。API 拒绝 superuser、BYPASSRLS、表／schema owner、危险角色继承及过宽写权限。迁移管理用户必须和运行用户分开。

Worker 控制角色使用 `pnpm db:grant-worker --role mender_worker`。`0008` 后它获得 execution.jobs／run_attempts 的必要读写、run_admissions 中 workspace/run/deployment_revision 三列读取，以及 `execution.runs` 的只读与 state/version/updated_at 列更新、`run_events` 仅 INSERT，用于在 fencing 与数据库 bundle 约束下记录供应商提交状态。它仍不能读取 canonical arguments、机器身份、预算/价格、Connection，不能改 Run 身份/创建时间，也不能访问 Outbox 或 cancellation facts。Worker startup 会再次验证角色，不接受 owner/runtime/admission/cancellation 角色替代。完整边界见 [Worker 租约记录](../../docs/engineering/2026-09-10-worker-leases.md)和 [供应商提交协议](../../docs/engineering/2026-09-10-supplier-submission.md)。

已有环境应用 `0008` 后必须重新执行 `pnpm db:grant-worker --role <原 worker role>`，否则 WorkerRole 启动校验会因缺少 submission 状态所需的窄权限而失败关闭。该命令不授予任何 supplier credential、commerce、connections 或 catalog 访问。

已有环境应用 `0007` 后还应重新执行 `pnpm db:grant-admission --role <原 admission role>`。该命令会主动撤销旧的 `execution.jobs` 整表 INSERT，再只授予 StartRun 所需的 workspace/run/state/blocked_reason/available_at/created_at/updated_at 列，防止旧 admission role 因新列出现而获得 priority、lease、fencing 或 attempt 配置写入能力。

启用“已 activation 但从未 lease 的 queued Job”协调取消后，也应重新执行 `pnpm db:grant-cancellation --role <原 cancellation role>`。该授权仅增加 `run_attempts` 的 workspace/run/attempt_no 三列读取用于证明 Attempt 不存在，并允许同步 Job `updated_at`；任何已有 Attempt 的 Job 都不会走即时额度释放路径。

真实数据库验证入口为 `pnpm test:integration`，缺少配置明确失败；本机自有临时资源入口为 `pnpm test:integration:docker`。截至协调取消收尾，Docker 环境阻塞已解除，扩展真实套件已通过；历史环境限制保留在[身份持久化记录](../../docs/engineering/2026-09-09-identity-postgres.md)，当前证据见[协调取消记录](../../docs/engineering/2026-09-10-coordinated-cancellation.md)。
