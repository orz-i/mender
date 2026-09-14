# Session：Trusted Provider Callback / Inbox Alpha

**Session id:** ses_3d423882ee9a4eaa8daa05aa4f6ff6c3
**Created:** unix:1789358947
**Updated:** unix:1789372492
**Status:** completed
**Host session scope:** host-session:f469a465ece78120da5de60df27407ac2fb02c786dfd8fc9bd42ee7a3f475fab
**Parent session id:** ses_2ff604d30e7b40b0bc4220d7837fb59e

## 用户核心目标

- 实现 Trusted Provider Callback / Inbox Alpha：为现有 HTTP/Remote Agent 异步执行主链增加受审 Provider callback ingress。HTTP 层必须对原始 body 做时间戳 HMAC 验签后才进入 durable Inbox；Inbox 按 provider+event_id 幂等去重并记录 accepted/quarantined 结果。合法回调必须通过既有 Attempt provider_id/provider_request_id/external_task_id 身份重新绑定后，复用 ProviderResults 收敛到 Run/Job/Artifact；重复、过期签名、对象归属不匹配、乱序或冲突不得推进执行状态。密钥仅来自显式 mounted secret provider，支持 reviewed key-id 重叠窗口，不从请求或数据库投影回传。增加受保护 Admin callback inbox 安全投影用于查看 accepted/quarantined/duplicate 元数据，不暴露 raw body、signature、secret、result payload。当前阶段不实现用户自定义出站 Webhook、任意 callback URL 配置、消息总线、A2A、多轮 Agent 输入、支付或生产部署。每个 slice 独立 commit，最终完整验证；不 push、不 deploy。

## 已确认事实


## 已完成修改


## 关键设计决定


## 测试结果


## 当前运行状态


## 剩余问题


## 下一步


## 本轮检查点

### auto-8dd162f6bcd3af09

```json
{
  "turn_id": "auto-8dd162f6bcd3af09",
  "timestamp": "unix:1789359889",
  "user_intent": "",
  "findings": [],
  "decisions": [],
  "files_changed": [],
  "tests": [],
  "runtime_state": [],
  "remaining_issues": [],
  "next_actions": [],
  "notes": ""
}
```

### close-work-session-69e5c486a9224037b911cd0c7746a5bf

```json
{
  "turn_id": "close-work-session-69e5c486a9224037b911cd0c7746a5bf",
  "timestamp": "unix:1789372492",
  "user_intent": "实现 Trusted Provider Callback / Inbox Alpha：为现有 HTTP/Remote Agent 异步执行主链增加受审 Provider callback ingress。HTTP 层必须对原始 body 做时间戳 HMAC 验签后才进入 durable Inbox；Inbox 按 provider+event_id 幂等去重并记录 accepted/quarantined 结果。合法回调必须通过既有 Attempt provider_id/provider_request_id/external_task_id 身份重新绑定后，复用 ProviderResults 收敛到 Run/Job/Artifact；重复、过期签名、对象归属不匹配、乱序或冲突不得推进执行状态。密钥仅来自显式 mounted secret provider，支持 reviewed key-id 重叠窗口，不从请求或数据库投影回传。增加受保护 Admin callback inbox 安全投影用于查看 accepted/quarantined/duplicate 元数据，不暴露 raw body、signature、secret、result payload。当前阶段不实现用户自定义出站 Webhook、任意 callback URL 配置、消息总线、A2A、多轮 Agent 输入、支付或生产部署。每个 slice 独立 commit，最终完整验证；不 push、不 deploy。",
  "findings": [],
  "decisions": [],
  "files_changed": [],
  "tests": [],
  "runtime_state": [],
  "remaining_issues": [],
  "next_actions": [],
  "notes": "Trusted Provider Callback / Inbox Alpha completed in three committed slices. Raw-body HMAC verification and durable FORCE-RLS Inbox were added with reviewed mounted key rotation; verified callbacks atomically rebind existing Attempt identity and converge through existing ProviderObservation/Run/Job/Artifact with duplicate and quarantine semantics; Admin gained an independent least-privilege callback-observer role plus safe typed Inbox projection/UI without raw body, signatures, secrets, provider handles, result/error payloads or client-side Run outcome inference. Product commits: 63044cbb8a78fca7a7bdfb53effe4f5191af308f, 23031c3d8a3c8a459fa8a28bdd86f52c5ac40a54, c35ff34df316bbc66ee31eb3ba38297bf3b4b7ff. Final required verifications pnpm-check, go-race, postgres-integration and git-diff-check all passed. No push or deploy."
}
```

