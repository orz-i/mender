# S4-03 Commerce Billing / Adjustments / Refunds｜T20／T21：COMPLETE

S4-03 按原始 roadmap 的固定编号实现账单聚合、调账、退款补充分录与对账后端工具。本工作包不再使用 S4-E 之类的实现批次编号；S4 的 canonical 范围保持为原设计中的 `S4-01..S4-18`，不会因为实现细节新增 S4-19 或继续数字膨胀。

## 与 S2 Usage Settlement 的边界

现有 `commerce.reservations`、`commerce.budget_periods` 与 `commerce.usage_settlements` 继续表示配额／用量结算事实，不被改造成资金账本。S4-03 新增独立的 operational billing ledger：`commerce.billing_journals` 与 `commerce.billing_entries`。一次已成立的 usage settlement 通过数据库 trigger 追加唯一的 `charge` journal；历史 usage settlement 现在也受到 UPDATE/DELETE immutability trigger 保护。

这意味着“修正账单”不能回写原 settlement，也不能改写旧 journal。退款和调账只能追加新的 supplemental journal，并保留原始业务依据。

## T20：复式、不可变、幂等

每个 billing journal 固定两条 entry，金额使用 integer micro-units，同一 journal 只能有一个币种，deferred constraint trigger 要求两条 entry 的 `delta_micro` 之和严格为 0。Charge、Refund、Debit Adjustment、Credit Adjustment 的账户和正负号都有数据库级精确形状约束。

`workspace_id + business_key` 唯一；usage settlement 另有“每个 Run 只能有一个 charge journal”的唯一索引。完全相同的 charge/refund 重放返回同一事实，不重复追加；相同 business key 但金额、业务依据、approval、actor 或 reason 漂移会冲突。

`billing_journals`、`billing_entries`、`usage_settlements` 均禁止 UPDATE/DELETE。账单 summary 的 net amount 从 `workspace_receivable` entries 重建，不能由调用方传入总额。

## Refund / Adjustment 与 S4-02 Dangerous Approval

S4-03 复用 S4-02 的 maker/checker 模型，而不是新造第二套审批系统。新增危险动作只有 `commerce.refund` 与 `commerce.adjustment`。

Platform `operator` 可以请求；`reviewer/operator` 可以独立复核，但 requester 不能 self-review。Approval 精确绑定 business key、business basis、direction、amount_micro、currency、parameters digest 与 expiry。Billing mutation 只能消费状态为 approved、未过期、requester/actor 一致且所有绑定完全一致的 approval，并且只消费一次。

失败的精确绑定、business-key conflict 或超额 refund 都在同一数据库事务内回滚，因此不会误消费 approval。Partial refund 会累计历史 refund，累计值不能超过原 `usage_settlement.charged_micro`。

独立 `billing-manager` 角色只拥有受控 summary/reconciliation/refund/adjustment 函数 EXECUTE；没有 billing ledger、usage settlement、reservation、budget、dangerous approval、Platform Staff、Run/Admission 表的直接业务访问。请求审批仍由独立 `dangerous-operation-manager` 执行，两种数据库身份互不替代。

## T21：未知 Provider 结果与晚到事实

Provider outcome 未知时继续使用既有 Execution `reconciling` 状态。Settlement application 明确拒绝 `reconciling` outcome；held reservation 不因 TTL 自动 release，billing ledger 也不产生 charge。

晚到成功结果必须先满足既有 Execution 完整性：durable unknown attempt、provider terminal observation、terminal Run、finished Job 和 immutable provider-result Artifact 在受约束事务中一致收敛。只有之后 Commerce settlement 成立，`usage_settlement` INSERT 才自动追加唯一 charge journal。因此晚到事实是“追加并收敛”，不是回写旧账。

## 对账与 API

Admin backend 暴露只读 billing summary / reconciliation，以及 approved refund / adjustment。Read 要求 `platform:audit`；mutation 要求 `platform:operate`、Browser Session 和 CSRF。金额以字符串进入 HTTP boundary 后解析为 int64 micro-units，未知 JSON 字段被拒绝，actor/result state 由服务端确定。

Reconciliation 对比 immutable usage-settlement charged total 与 ledger charge total，同时报告缺失 charge journal 与仍处于 `reconciling` 的 Run 数。它不是 Payment Provider reconciliation，也不声称已与真实资金通道对账。

## 明确不在 S4-03 中伪装完成的内容

A11 要求 PriceVersion、MeterEvent、Ledger、ProviderCostEvent 四类事实保持分离；本工作包没有实现独立 ProviderCostEvent accounting，因此 machine evidence 明确标记为 deferred。Payment Provider adapter/callback、real-money wallet funding、自动 FX、税务计算、statutory accounting general ledger、Billing frontend 与 production deploy 同样不属于本次完成声明。

S4-03 完成后不自动发明“下一 S4-x”。下一项开发必须重新对照 canonical roadmap `S4-01..S4-18` 的现有条目和完成状态选择。
