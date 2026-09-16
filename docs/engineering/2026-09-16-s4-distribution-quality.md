# 原 S4-07 / S4-08 / S4-16：分发、质量与制品工具

沿用原任务，不新增阶段。CLI、Skill、健康与制品工具都消费现有合同，不开放新的权限或任意代码运行时。

## CLI / Skill

`backend/cmd/mender` 调用 `internal/platform/mcpconsumer`，只依赖官方 Go MCP SDK，不 import 业务上下文。`pnpm cli --help` 查看用法；[Skill](../skills/mender/SKILL.md)提供调用引导。CLI 的 list/inspect 是当前授权工具集的发现，非全站语义搜索；call 复用现有 tools/call 和服务器权限、幂等、预算。所有业务调用需显式 `--execute`，参数通过 stdin，key 仅从受限本地文件读取。不自动重试、不跟随重定向、不使用环境代理。

单元测试覆盖真实SDK发现、显式调用、精确整数、schema漂移、调用前确认、凭据反射与重定向拒绝。PostgreSQL MCP集成增加CLI受理/重放/查询/取消/只读Key及跨租户拒绝，证明入口一致性而非复制假结果。

## 发布质量 / 只读健康检查

`pnpm cli ... list` 输出排序后的工具合同、协议版本和SHA-256快照；保存后由发布负责人审阅。`pnpm cli ... --baseline SNAPSHOT health` 对真实配置端点只执行发现，比对合同摘要，失败返回非零退出，不覆盖快照，不试调用有副作用工具。每页、工具数量、响应大小和超时有上限。

`pnpm quality:probe CONFIG.json` 通过CLI执行1～60次有界采样，间隔5～60秒；配置固定endpoint/key_file/baseline/samples/interval_ms。只读探测记录UTC、延迟、合同摘要及状态，任一样本失败使整个任务返回非零，不把上游原始错误或秘密复制到报告。任务可由现有作业调度器显式启动，不会安装后台服务。

`pnpm quality:report REPORT.json` 输出无脚本、受CSP限制的只读HTML质量视图，支持浏览器查看采样趋势和漂移/不可用状态。报告不包含凭据、请求内容或发布按钮。示例配置文件由操作者填写已授权端点；不自动开始网络调用。

它适用于原S4受控工具集的人工/CI/运维调度探测；当前不提供平台内多租户质量趋势数据库或自动灰度晋级。上游功能质量须用有授权且费用受控的合成任务另行测试；本工具不擅自执行。计划、执行器与失败规则有单元测试，真实CLI/授权/预算链路另由PostgreSQL合同验证，不能把两者说成外部供应商认证。

## 制品校验与签名

```sh
pnpm release:artifact inventory FULL_SOURCE_SHA frontend/apps/console/dist/index.html pnpm-lock.yaml backend/go.mod backend/go.sum
pnpm release:artifact sign REVIEWED_INVENTORY_JSON PRIVATE_KEY_FILE
pnpm release:artifact verify SIGNED_ENVELOPE_JSON TRUSTED_PUBLIC_KEY_FILE
```

每条命令只输出JSON，不写文件、不生成密钥、不发布制品；需要保存时由操作者选择输出文件。清单必须显式枚举全部待发布文件，示例只是参数演示，不能把只签index.html当成签了整个应用。inventory记录实际读取的字节摘要，source_revision为审阅者提供的来源锚点，不自动证明工作区clean或可复现构建。

Ed25519签名绑定完整清单、来源revision和有效期；验证者必须另行提供可信公钥，不信任制品自带公钥。拒绝未知字段、路径穿越、ADS、设备路径、大小超限、符号链接、重复/大小写冲突、敏感路径、过期、错签及内容漂移。签名仅证明指定密钥签了这些字节，不证明代码安全、发布已审批或供应商授权。

生产私钥托管、轮换、硬件保护和可信公钥发布属于运维批准事项；本轮仅使用临时测试密钥，不提交生产密钥。Windows密钥文件ACL由操作者控制，POSIX入口检查私钥/访问key不可供其他用户读取。部署前必须对最终不可变副本重新验证；本地校验不能消除验证后文件被改写的风险。

## 依赖扫描

`pnpm check:dependencies` 单独执行pnpm advisory、Go模块完整性和固定版本govulncheck（带明确包参数与-test）。扫描网络不可用、解析失败和漏洞退出都保持失败，不用JSON模式的零退出伪装无漏洞。不会自动upgrade、audit fix或跳过告警。正常`pnpm check`保留本地可执行回归；网络依赖扫描独立进入CI。

依据官方Node crypto、Go SDK和Go vulnerability文档的接口实现，保持项目既有工具链。当前版本兼容性以实际运行结果为准；离线制品单元测试不等于完成了在线依赖扫描。

## 验证与限制

工具单测入口 `pnpm test:s4:tools`；真实数据库/browser使用仓库既有隔离入口。最终执行时间、命令、结果与剩余范围见本轮G4汇总，不把历史证据文件改成当时已经完成的新功能。
