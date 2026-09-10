# 数据迁移

当前迁移为 `0001–0005`。`0005_coordinated_cancellation.sql` 支持已受理但尚未执行任务的原周期预留释放、Job 停止和取消事实同事务提交；详见[协调取消收尾记录](../../docs/engineering/2026-09-10-coordinated-cancellation.md)。应用既有库迁移仍由操作员显式执行，不在 API 启动时自动执行。

本轮新增 `0004_atomic_admission.sql`：commerce 预算期／额度预留和 execution 受理／blocked Job／Outbox，含 FORCE RLS 与延迟额度总和约束。0001–0003 内容不变。已有开发数据库应用新增迁移后须重新执行 `pnpm db:grant-runtime --role <查询运行角色>`，仅补充受理关联的两列读取权；不能将 admission writer 或迁移所有者混入查询 API。详情及真实故障注入结果见 [内部原子受理](../../docs/engineering/2026-09-09-atomic-admission.md)。

`0003_execution_read_indexes.sql` 归 execution 所有，为 Workspace＋创建时间＋C 排序 ID 以及状态筛选建立 keyset 索引；原 0001／0002 内容未覆盖。该子集已随隔离 PostgreSQL 套件验证；当前不承诺新旧版本滚动兼容。

已有两个顺序迁移：`0001_identity.sql` 拥有工作区／服务账号／机器 Key；`0002_execution.sql` 拥有 Run／操作者事件及 Workspace RLS。元信息在 `mender_meta`，仅操作员写入；API 启动只核验版本、摘要和受限角色，不执行迁移。

从仓库根使用 `pnpm db:migrate`，仅读取 `MENDER_ADMIN_DATABASE_URL`。代码按事务＋锁执行并校验已应用摘要，换行归一化；不能覆盖已应用 SQL 或手动修改摘要。没有破坏性自动 down migration，回退需单独评审。

`pnpm db:grant-runtime --role mender_app` 只授予已有角色必要的 SELECT、Run 特定列 UPDATE 和事件 INSERT，不创建角色／密码，不清理历史宽授权。API 拒绝 superuser、BYPASSRLS、表／schema owner、危险角色继承及过宽写权限。迁移管理用户必须和运行用户分开。

真实数据库验证入口为 `pnpm test:integration`，缺少配置明确失败；本机自有临时资源入口为 `pnpm test:integration:docker`。截至协调取消收尾，Docker 环境阻塞已解除，扩展真实套件已通过；历史环境限制保留在[身份持久化记录](../../docs/engineering/2026-09-09-identity-postgres.md)，当前证据见[协调取消记录](../../docs/engineering/2026-09-10-coordinated-cancellation.md)。
