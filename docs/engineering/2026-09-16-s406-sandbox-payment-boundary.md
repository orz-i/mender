# S4-06 / FR-019 / T27 Sandbox Payment Adapter & Callback Reconciliation：COMPLETE

S4-06 按 canonical roadmap 交付支付供应商适配与回调对账的 **sandbox boundary**。设计基线尚未确认地区、币种覆盖、商户资格或生产 PSP，因此本阶段不选择或伪装真实商户上线：`mode` 在数据库与配置层都只能是 `sandbox`，`live` 配置直接失败关闭。

## 与 S4-03 的事实分离

S4-03 `billing_journals` 继续是不可变 operational billing ledger。S4-06 `payment_intents` 只表达“某个既有 charge/refund journal 需要由外部 PSP 收款/退款”的执行意图。`collect_charge` 必须精确绑定 charge journal；`execute_refund` 必须精确绑定 refund journal，金额和币种必须完全一致。

PSP callback 成功只把 PaymentIntent 从 `pending` 推进为 `settled`，失败推进为 `failed`。它**不会创建新的 billing journal，也不会 UPDATE/DELETE 原 charge/refund journal**。浏览器 redirect、checkout success 页面或客户端状态都不是 payment truth，仓库没有这类入账端点。

## Callback 真源与幂等

外部 callback 使用 HMAC-SHA256；签名域包含 provider、provider account、时间戳和原始 body，并限定五分钟窗口。应用层先验签，之后才 canonicalize/解析 payload 并调用独立 callback-ingestor 数据库角色。签名失败不会到达 receiver 或数据库。

`payment_callback_inbox` 只保存验签后的安全字段和 body SHA-256，不保存 raw body、raw signature、secret、Authorization header 或支付凭据。幂等 namespace 为 `provider/account/event_id`：完全相同的重投只增加 delivery evidence；相同 event ID 事实漂移会以新 receipt 保存为 `quarantined:event_id_conflict`，不会再次推进 intent。

Provider/account、workspace/intent、event type、amount、currency 任一与 immutable intent 不一致都进入 quarantine。未知 intent、终态后的 late event 也不会重放资金动作。

## Restricted roles 与安全投影

`payment-manager` 仅能调用 sandbox intent create、safe intent/callback projection 和 reconciliation functions；`payment-callback-ingestor` 仅能调用 verified callback ingest。两者都不能直接 SELECT/INSERT/UPDATE/Delete payment、billing、Identity 或 Execution 业务表，也不能互换能力。

Admin API 使用 Browser Session；创建 intent 强制 CSRF。金额以 decimal string 传输。Callback Admin projection 不暴露 body hash、key ID、provider transaction ID、签名或 secret，只返回事件 ID、intent、类型、金额币种、状态、disposition、reason、时间与 delivery count。

## T27 真实证据

`backend/tests/integration/sandbox_payment_callbacks_test.go` 在真实临时 PostgreSQL restricted roles 上证明：live mode 被拒绝；intent 必须精确绑定 S4-03 journal；成功 callback 只结算一次；精确 duplicate 为 replay；同 event ID 漂移、金额/币种错配、未知 intent、late terminal event 被隔离；failed callback 只失败对应 intent；refund provider confirmation 不改写原 refund journal；reconciliation 从 immutable intents/callbacks 重建 expected/settled/difference，并按币种隔离 quarantine。

`paymenthmac/verifier_test.go` 证明签名同时绑定 provider/account/timestamp/body，任一漂移都拒绝；application test 证明 verifier 失败时 receiver 调用数为零。

## 明确 deferred

S4-06 **不是 production payment readiness**。真实 PSP 网络调用、生产商户 credentials、真实钱包充值、FX、税务/statutory accounting、供应商打款、S4-11 billing/payment frontend 和 production deploy 仍未实现。后续开发必须回到 canonical `S4-01..S4-18` 矩阵重新选择未完成条目，不能创造新的 S4 编号。
