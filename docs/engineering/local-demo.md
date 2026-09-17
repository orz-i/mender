# Mender 本地可用测试实例

这个入口用于开发者在**自己的 Docker Desktop** 中持续使用 Mender。它保留数据库卷、使用真实 Console／Admin HTTPS 入口，并提供一个只允许 `maker`／`checker` 两个身份的本地 OIDC 测试服务。

它不是生产身份系统，不连接真实支付或真实供应商。`local.demo` 配置会显式启用一个**仅本机**的 `Company lookup` 演示能力，用于验证 Tools → Run → Worker → Usage 的真实产品链路；该 fixture 只允许 `ws_local`、loopback HTTP 和固定 deployment revision，production 配置会拒绝它。

## 一次性准备

从一个已经构建且审阅的 release 开始：

```powershell
Set-Location D:\mender
pnpm run deploy build dist/releases/local-use
pnpm run deploy prepare deploy/local.demo.example.json dist/releases/local-use .local/deploy/mender-demo
pnpm run deploy preflight .local/deploy/mender-demo
pnpm run deploy up .local/deploy/mender-demo --apply
pnpm run deploy operator .local/deploy/mender-demo seed-local-demo --workspace ws_local --user user_maker
```

`prepare` 会在 `.local/deploy/mender-demo` 内生成数据库密码、OIDC client secret、私有 CA 与服务证书；这些文件被 Git 忽略，不应复制到聊天或提交仓库。嵌入式 OIDC 只在 `environment=local` 且 `local_oidc_fixture=true` 时生成，production 配置会拒绝它。

默认入口：

```text
Console  https://console.localhost:18443
Admin    https://admin.localhost:18443
OIDC     https://idp.localhost:19443
```

浏览器需要信任 `.local/deploy/mender-demo/local-ca.crt` 才能正常打开这些 HTTPS 地址。不要关闭系统 TLS 校验；如果不想把测试 CA 加到系统信任，可使用单独浏览器配置并仅信任这个本地 CA。

## 建立两个本地身份

部署启动后执行：

```powershell
pnpm run deploy operator .local/deploy/mender-demo provision-human --user user_maker --display-name Maker --issuer https://idp.localhost:19443 --oidc-subject maker --workspace ws_local --membership-role owner
pnpm run deploy operator .local/deploy/mender-demo provision-platform-staff --user user_maker --platform-role operator

pnpm run deploy operator .local/deploy/mender-demo provision-human --user user_checker --display-name Checker --issuer https://idp.localhost:19443 --oidc-subject checker --workspace ws_local --membership-role owner
pnpm run deploy operator .local/deploy/mender-demo provision-platform-staff --user user_checker --platform-role reviewer
```

Console 登录选择 **Maker**；Admin 需要独立登录时选择 **Checker**。两个入口是不同 host，Console Cookie 不会自动成为 Admin Cookie。

`seed-local-demo` 是幂等的，只能在 `MENDER_LOCAL_DEMO_ENABLED=true` 的 local fixture 中使用。它创建一个本地 Tool、Connection、Price、Budget、Toolset 以及低风险 read-only 执行策略，不会创建外部供应商凭据，也不会改变普通新 Workspace 默认 fail-closed 的治理规则。

## 日常使用

查看状态：

```powershell
pnpm run deploy status .local/deploy/mender-demo
```

停止但保留数据：

```powershell
pnpm run deploy stop .local/deploy/mender-demo
```

再次启动：

```powershell
pnpm run deploy up .local/deploy/mender-demo --apply
```

不要再次运行 `prepare`，它会拒绝覆盖已有部署。数据库卷和部署秘密应成套保留；只保留数据库而丢失角色密码或 CA，不构成完整恢复。

## 当前可测试范围

可以实际测试 Workspace、授权连接、Tools、Run／Usage、Publisher，以及 Admin 的发布审核、发布历史、策略、执行治理、平台运营、JIT 和 sandbox 账务页面。

Maker 登录后，`Tools` 页面会看到 **Company lookup**。点击 **Run tool**，输入例如 `Acme`，Mender 会走真实 OIDC Session → execution policy → Human delegation → StartRun → Worker → 本地 Provider → settlement。成功后可以在 Runs 和 Usage 中看到结果和费用；演示价格为每次成功 `5,000 micro USD`，预算和费用都只存在于这个本地 Workspace。

本地 OIDC 只证明 Mender 会话、PKCE／nonce／JWKS 处理和角色分离可工作；不能替代真实企业 IdP 验收。sandbox 支付不能被描述为真实充值。`Company lookup` 只访问 Worker 网络命名空间里的本地 fixture，不代表真实第三方供应商已经接入。

## 清理

普通停止不要删卷。如果明确决定删除这个测试实例，先备份需要的数据并人工核对 `mender_demo` 的 ownership 标记，再使用该实例自己的 Compose 文件处理；不要运行全局 `docker system prune` 或删除其他项目资源。

