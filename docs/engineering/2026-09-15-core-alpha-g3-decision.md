# Mender Core Alpha / G3 Closure Decision

日期：2026-09-15  
机器决议：[alpha-closure.json](alpha-closure.json)  
G3 能力证据：[g3-evidence.json](g3-evidence.json)  
运维边界证据：[alpha-operations-evidence.json](alpha-operations-evidence.json)

## 决议

**Core Alpha：COMPLETE**  
**G3 内部集成 Gate：SCOPED GO**

这里的 `SCOPED GO` 表示 Mender 当前 Core Alpha 范围内的三类来源、两类 MCP 分发、OAuth 生命周期、Remote Agent、Provider Callback、Artifact Object 与取消语义已经达到可执行的内部集成 Gate；它不等于第三方公网客户端、生产代理、生产告警或商业支付已经认证。

## G3 已通过的范围

同一个执行内核已经有真实自动化证据覆盖：

- HTTP / API as Tool；
- upstream MCP / MCP Tool；
- reviewed `agent_http` / Agent as Tool；
- Meta-tool MCP 与 Fixed Toolset MCP；
- OAuth T07 / T08；
- MCP T13 / T14 / T15 / T16；
- Remote Agent T17；
- Provider Callback T28；
- Artifact Object T30；
- MCP cancellation T40。

三类入口继续共享 Admission、Governance、Run、Job、Attempt、Artifact 与 settlement 真源；不会因为 adapter 类型另造任务系统或最终状态。

## S3-18 客户端验收名单

当前唯一认证项是：

| Client / Harness | 版本 | 协议 | 分发 | 状态 |
| --- | --- | --- | --- | --- |
| `github.com/modelcontextprotocol/go-sdk` automated harness | v1.7.0 | 2026-07-28 | Meta-tool + Fixed Toolset | certified |

`third_party_clients_certified = []`。当前没有把 Claude、Cursor 或其他第三方 MCP 客户端列入认证名单；选定 legacy `2025-11-25` 仍明确 rejected。

本阶段也没有真实外部试点用户反馈，因此 `user_feedback_status` 明确记录为 `not-collected-external-acceptance-deferred`。这不会阻止仓库内 **Core Alpha / internal G3** 收口，但会阻止任何“外部客户端 interoperability 已完成”或“已有用户验收”的表述。

## 运维边界

S3-16 / S3-17 按 Alpha bounded scope 收口：exact host allowlist、默认禁止明文 HTTP / loopback、Deployment timeout、Artifact retention、physical delete、callback durable Inbox 和 Admin safe projection 均有机器证据。

以下能力仍 deferred：production streaming proxy、standalone MCP SSE、application RunEvent SSE、external callback alert transport、cloud object storage certification。当前版本因此不是 production-ready，也没有完成公网 proxy / alert SLA 认证。

## 明确不属于 Core Alpha 完成声明

- 第三方 MCP client interoperability；
- 外部用户 acceptance / feedback；
- A2A protocol；
- multi-turn Agent conversation / orchestration；
- production streaming reverse proxy / standalone SSE / application SSE；
- external callback paging / alert transport；
- S3 / MinIO / cloud object storage certification；
- payment accounting、钱包、退款、供应商分账；
- 任意插件代码、脚本或动态 sandbox runtime；
- 生产部署、容量、灾备、RPO/RTO、on-call 和 G5 上线认证。

## 下一阶段

Core Alpha 收口后可以进入 **S4 governed commercialization preparation**，继续做发布清单、灰度/回退、商业账务与分发治理；同时第三方客户端 interoperability 和生产 SRE 能力作为独立准入项推进。任何 deferred 项在真实证据完成前都不得反向修改本决议为 certified。
