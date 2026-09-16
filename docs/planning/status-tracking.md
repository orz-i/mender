# 实施状态与验收证据维护

## 单一状态源

`project-data.json` 保留原 92 个任务、范围、依赖、验收条件和估算。每项 `execution` 是唯一可编辑实际状态；顶层 `status`、`progress`、`evidence` 是兼容投影，必须保持一致。`baseline_execution` 原样保存初始状态，仅供历史比较。

`current-status.md` 由 `node scripts/render-project-status.mjs --write` 生成。不得手改生成页来隐藏待交付项；状态页和基线有分歧时先修主台账。历史设计、Office／PDF 文件没有自动同步，本轮明确归档为 v1.1 快照，不再将旧 0% 描述为当前进度。

## 三个独立状态

| 维度 | 含义 |
| --- | --- |
| implementation | 待复核、明确未开始、部分实现、限定范围实现、原任务范围实现 |
| verification | 待复核／未执行、已有历史记录、具有具体执行回执的通过或失败 |
| acceptance | 原任务待验收、明确保留缺项的 scoped-go、完整验收 |

本次回填采用本地源码及既有记录，`recorded` **不表示本次重新运行成功**。真正通过时应另附任务、命令、提交、环境、执行时间、结果与日志回执。源码中存在测试函数、JSON 写着 COMPLETE、检查器返回 PASS，都不能自动转换成产品验收。

限定范围实现的原任务通常是“待验收”，不是“已完成”。待复核不能推断为没有代码。`progress = null` 表示没有经批准的完成权重；仅在完整验收、无剩余条件时为 1。不得为了恢复 Excel 汇总而把未知进度强填 0 或 100%。

## 可复核证据

引用必须是仓库内真实文件，记录路径及 SHA-256。摘要改变时，检查会要求重新查看范围和剩余条件；不能只刷新 hash 而不复核。摘要只证明被引用内容未漂移，不证明内容陈述或测试结论真实。

`reviewed_head` 是本次读取源码的本地提交，不是永远等于运行检查时 HEAD 的魔法标签。新实现影响被引用文件时需要更新相关映射和复核记录。对尚未纳入索引的源码变化，此检查不提供全仓完整性保证。

原计划结构以 fingerprint 防止无意改变编号、依赖、范围和人日。合法范围变更必须更新原计划、决议与 fingerprint，并由正常评审确认；这不是防御恶意维护者的安全机制。

## 历史范围和新实现

旧 Alpha 决议中 payment-accounting 为 deferred，在 S4-03 新增业务账本后，不回写旧决议来伪造提前完成。由当前台账明确“哪个原任务、在什么范围已实现”。旧 S4-C 的财务消费者 deferred 同样由 S4-03 新证据补充，不改变旧工作包的当时含义。

S4-01～06 的后端与 sandbox 范围不能顺带完成 S4-09～11 的页面；S4-13／14 的后台测试不能顺带获得浏览器或第三方验收。保留这些具体剩余项是停止阶段膨胀的必要条件。

## 复核与更新

修改实际状态及证据后生成当前页，再运行状态负向测试和 `pnpm check`。触及业务逻辑时按影响运行实际 Go／数据库／浏览器套件，不能复用旧提交成功结论。更新验收字段需要独立决议记录；远端合并保护和生产权限保持人工／运营责任，不由仓库内容决定。

## 提升状态需要的独立记录

当前条目使用 `recorded`，没有伪造执行或批准记录。以后提升为 `passed`，`verification_receipt` 必须引用真实 JSON，包含 `record_type: task-verification`、原 task_id、command、environment、head、finished_at、result、exit_code 和带路径／摘要的 log。日志需来自对应命令实际运行，不能引用仅声明测试文件名的 scope manifest。

完整 `accepted` 还需原范围 implementation=implemented、remaining 为空及独立的 `task-acceptance` 记录。记录包含 task_id、decision=accepted、coverage=task-full-scope、approved_by、approved_at、scope，并绑定同一 verification_receipt。仅检查这些字段不能鉴别人类身份或证明原验收语义完整，仍由真实评审人负责；负向测试的内存 fixture 不写入这些生产记录。

G3 已有内部 scoped-go 作为历史决议保留；G4／G5 的 go 需要各自原阶段全部任务的完整验收记录，不能引用单一后端切片。若要正式改变 Gate 的范围化放行规则，先修订决议与状态 schema，不擅自删除剩余任务。外部条件提升为 verified 需要独立 `external-control-verification`，包含 control_id、external_reference、verified_by、verified_at 和 result，不接受本地 CI／支付模拟 JSON 代替远端观察。
