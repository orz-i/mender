# Session：Trusted Provider Callback / Inbox Alpha

**Session id:** ses_3d423882ee9a4eaa8daa05aa4f6ff6c3
**Created:** unix:1789358947
**Updated:** unix:1789359809
**Status:** active
**Host session scope:** host-session:f469a465ece78120da5de60df27407ac2fb02c786dfd8fc9bd42ee7a3f475fab
**Parent session id:** ses_2ff604d30e7b40b0bc4220d7837fb59e

## 用户核心目标

- 实现 Trusted Provider Callback / Inbox Alpha：为现有 HTTP/Remote Agent 异步执行主链增加受审 Provider callback ingress。HTTP 层必须对原始 body 做时间戳 HMAC 验签后才进入 durable Inbox；Inbox 按 provider+event_id 幂等去重并记录 accepted/quarantined 结果。合法回调必须通过既有 Attempt provider_id/provider_request_id/external_task_id 身份重新绑定后，复用 ProviderResults 收敛到 Run/Job/Artifact；重复、过期签名、对象归属不匹配、乱序或冲突不得推进执行状态。密钥仅来自显式 mounted secret provider，支持 reviewed key-id 重叠窗口，不从请求或数据库投影回传。增加受保护 Admin callback inbox 安全投影用于查看 accepted/quarantined/duplicate 元数据，不暴露 raw body、signature、secret、result payload。当前阶段不实现用户自定义出站 Webhook、任意 callback URL 配置、消息总线、A2A、多轮 Agent 输入、支付或生产部署。每个 slice 独立 commit，最终完整验证；不 push、不 deploy。

## 已确认事实

- 自动阶段检查点：tool=exec_command, status=succeeded, success=true
- command=pwsh -NoLogo -NoProfile -NonInteractive -Command "pnpm check:architecture"
- command=pwsh -NoLogo -NoProfile -NonInteractive -Command "node scripts/backend.mjs fmt ./internal/processes/providercallback/... ./tests/architecture"
- command=pwsh -NoLogo -NoProfile -NonInteractive -Command "pnpm test:integration:docker"

## 已完成修改

- backend/internal/processes/providercallback/application/service.go

## 关键设计决定


## 测试结果

- verification_kind=architecture, success=true
- verification_kind=test, success=true

## 当前运行状态

- task_id=69e5c486a9224037b911cd0c7746a5bf
- task_status=active
- tool=exec_command
- session_id="5c52f2e0-b8c9-4235-8552-79cf33c97a51"
- execution_status="succeeded"
- exit_code=0
- last_output_at="2026-09-14T04:23:27.881Z"
- branch=main
- head=e3bca591122c5609514519a946ad9fabc8b8d951
- baseline_matches=Some(true)

## 剩余问题


## 下一步

- Slice 1: callback HMAC/raw-body contract, durable Inbox schema/role and strict HTTP ingress unit tests
- Slice 2: Inbox -> existing ProviderObservation/Run/Artifact atomic convergence, bootstrap composition and real PostgreSQL replay/ordering/ownership coverage
- Slice 3: protected Admin callback inbox projection + typed client/UI observability without raw payloads
- Run pnpm check, Go race, real PostgreSQL integration, git diff --check
- Strict complete_work_session; leave working tree clean

## 本轮检查点

### auto-exec_command-ff15a428e1f707b0

```json
{
  "turn_id": "auto-exec_command-ff15a428e1f707b0",
  "timestamp": "unix:1789359774",
  "user_intent": "实现 Trusted Provider Callback / Inbox Alpha：为现有 HTTP/Remote Agent 异步执行主链增加受审 Provider callback ingress。HTTP 层必须对原始 body 做时间戳 HMAC 验签后才进入 durable Inbox；Inbox 按 provider+event_id 幂等去重并记录 accepted/quarantined 结果。合法回调必须通过既有 Attempt provider_id/provider_request_id/external_task_id 身份重新绑定后，复用 ProviderResults 收敛到 Run/Job/Artifact；重复、过期签名、对象归属不匹配、乱序或冲突不得推进执行状态。密钥仅来自显式 mounted secret provider，支持 reviewed key-id 重叠窗口，不从请求或数据库投影回传。增加受保护 Admin callback inbox 安全投影用于查看 accepted/quarantined/duplicate 元数据，不暴露 raw body、signature、secret、result payload。当前阶段不实现用户自定义出站 Webhook、任意 callback URL 配置、消息总线、A2A、多轮 Agent 输入、支付或生产部署。每个 slice 独立 commit，最终完整验证；不 push、不 deploy。",
  "findings": [
    "自动阶段检查点：tool=exec_command, status=succeeded, success=true",
    "command=pwsh -NoLogo -NoProfile -NonInteractive -Command \"pnpm check:architecture\""
  ],
  "decisions": [],
  "files_changed": [],
  "tests": [
    "verification_kind=architecture, success=true"
  ],
  "runtime_state": [
    "task_id=69e5c486a9224037b911cd0c7746a5bf",
    "task_status=active",
    "tool=exec_command",
    "session_id=\"c75ee384-fe33-4523-b4d5-d61920385c2c\"",
    "execution_status=\"succeeded\"",
    "exit_code=0",
    "last_output_at=\"2026-09-14T04:22:52.765Z\"",
    "branch=main",
    "head=e3bca591122c5609514519a946ad9fabc8b8d951",
    "baseline_matches=Some(true)"
  ],
  "remaining_issues": [],
  "next_actions": [
    "Slice 1: callback HMAC/raw-body contract, durable Inbox schema/role and strict HTTP ingress unit tests",
    "Slice 2: Inbox -> existing ProviderObservation/Run/Artifact atomic convergence, bootstrap composition and real PostgreSQL replay/ordering/ownership coverage",
    "Slice 3: protected Admin callback inbox projection + typed client/UI observability without raw payloads",
    "Run pnpm check, Go race, real PostgreSQL integration, git diff --check",
    "Strict complete_work_session; leave working tree clean"
  ],
  "notes": "Anchor 自动保存的结构化阶段检查点；相同阶段身份会幂等更新。"
}
```

### auto-exec_command-607cda5b2b3546ac

```json
{
  "turn_id": "auto-exec_command-607cda5b2b3546ac",
  "timestamp": "unix:1789359745",
  "user_intent": "实现 Trusted Provider Callback / Inbox Alpha：为现有 HTTP/Remote Agent 异步执行主链增加受审 Provider callback ingress。HTTP 层必须对原始 body 做时间戳 HMAC 验签后才进入 durable Inbox；Inbox 按 provider+event_id 幂等去重并记录 accepted/quarantined 结果。合法回调必须通过既有 Attempt provider_id/provider_request_id/external_task_id 身份重新绑定后，复用 ProviderResults 收敛到 Run/Job/Artifact；重复、过期签名、对象归属不匹配、乱序或冲突不得推进执行状态。密钥仅来自显式 mounted secret provider，支持 reviewed key-id 重叠窗口，不从请求或数据库投影回传。增加受保护 Admin callback inbox 安全投影用于查看 accepted/quarantined/duplicate 元数据，不暴露 raw body、signature、secret、result payload。当前阶段不实现用户自定义出站 Webhook、任意 callback URL 配置、消息总线、A2A、多轮 Agent 输入、支付或生产部署。每个 slice 独立 commit，最终完整验证；不 push、不 deploy。",
  "findings": [
    "自动阶段检查点：tool=exec_command, status=succeeded, success=true",
    "command=pwsh -NoLogo -NoProfile -NonInteractive -Command \"node scripts/backend.mjs fmt ./internal/processes/providercallback/... ./tests/architecture\""
  ],
  "decisions": [],
  "files_changed": [
    "backend/internal/processes/providercallback/application/service.go"
  ],
  "tests": [],
  "runtime_state": [
    "task_id=69e5c486a9224037b911cd0c7746a5bf",
    "task_status=active",
    "tool=exec_command",
    "session_id=\"3273dbb5-2589-40f0-8342-8a20d25d136f\"",
    "execution_status=\"succeeded\"",
    "exit_code=0",
    "last_output_at=\"2026-09-14T04:22:22.955Z\"",
    "branch=main",
    "head=e3bca591122c5609514519a946ad9fabc8b8d951",
    "baseline_matches=Some(true)"
  ],
  "remaining_issues": [],
  "next_actions": [
    "Slice 1: callback HMAC/raw-body contract, durable Inbox schema/role and strict HTTP ingress unit tests",
    "Slice 2: Inbox -> existing ProviderObservation/Run/Artifact atomic convergence, bootstrap composition and real PostgreSQL replay/ordering/ownership coverage",
    "Slice 3: protected Admin callback inbox projection + typed client/UI observability without raw payloads",
    "Run pnpm check, Go race, real PostgreSQL integration, git diff --check",
    "Strict complete_work_session; leave working tree clean"
  ],
  "notes": "Anchor 自动保存的结构化阶段检查点；相同阶段身份会幂等更新。"
}
```

### auto-exec_command-609d07486983421d

```json
{
  "turn_id": "auto-exec_command-609d07486983421d",
  "timestamp": "unix:1789359809",
  "user_intent": "实现 Trusted Provider Callback / Inbox Alpha：为现有 HTTP/Remote Agent 异步执行主链增加受审 Provider callback ingress。HTTP 层必须对原始 body 做时间戳 HMAC 验签后才进入 durable Inbox；Inbox 按 provider+event_id 幂等去重并记录 accepted/quarantined 结果。合法回调必须通过既有 Attempt provider_id/provider_request_id/external_task_id 身份重新绑定后，复用 ProviderResults 收敛到 Run/Job/Artifact；重复、过期签名、对象归属不匹配、乱序或冲突不得推进执行状态。密钥仅来自显式 mounted secret provider，支持 reviewed key-id 重叠窗口，不从请求或数据库投影回传。增加受保护 Admin callback inbox 安全投影用于查看 accepted/quarantined/duplicate 元数据，不暴露 raw body、signature、secret、result payload。当前阶段不实现用户自定义出站 Webhook、任意 callback URL 配置、消息总线、A2A、多轮 Agent 输入、支付或生产部署。每个 slice 独立 commit，最终完整验证；不 push、不 deploy。",
  "findings": [
    "自动阶段检查点：tool=exec_command, status=succeeded, success=true",
    "command=pwsh -NoLogo -NoProfile -NonInteractive -Command \"pnpm test:integration:docker\""
  ],
  "decisions": [],
  "files_changed": [],
  "tests": [
    "verification_kind=test, success=true"
  ],
  "runtime_state": [
    "task_id=69e5c486a9224037b911cd0c7746a5bf",
    "task_status=active",
    "tool=exec_command",
    "session_id=\"5c52f2e0-b8c9-4235-8552-79cf33c97a51\"",
    "execution_status=\"succeeded\"",
    "exit_code=0",
    "last_output_at=\"2026-09-14T04:23:27.881Z\"",
    "branch=main",
    "head=e3bca591122c5609514519a946ad9fabc8b8d951",
    "baseline_matches=Some(true)"
  ],
  "remaining_issues": [],
  "next_actions": [
    "Slice 1: callback HMAC/raw-body contract, durable Inbox schema/role and strict HTTP ingress unit tests",
    "Slice 2: Inbox -> existing ProviderObservation/Run/Artifact atomic convergence, bootstrap composition and real PostgreSQL replay/ordering/ownership coverage",
    "Slice 3: protected Admin callback inbox projection + typed client/UI observability without raw payloads",
    "Run pnpm check, Go race, real PostgreSQL integration, git diff --check",
    "Strict complete_work_session; leave working tree clean"
  ],
  "notes": "Anchor 自动保存的结构化阶段检查点；相同阶段身份会幂等更新。"
}
```

