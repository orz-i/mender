# Mender 本地可用测试实例

这个入口用于开发者在**自己的 Docker Desktop** 中持续使用 Mender。它保留数据库卷、使用真实 Console／Admin HTTPS 入口，并提供一个只允许 `maker`／`checker` 两个身份的本地 OIDC 测试服务。

它不是生产身份系统，不连接真实支付或真实供应商，也不会自动制造 Tool、Connection、价格或预算。Worker 默认不执行外部供应商任务。

## 一次性准备

从一个已经构建且审阅的 release 开始：

```powershell
Set-Location D:\mender
pnpm run deploy build dist/releases/local-use
pnpm run deploy prepare deploy/local.demo.example.json dist/releases/local-use .local/deploy/mender-demo
pnpm run deploy preflight .local/deploy/mender-demo
pnpm run deploy up .local/deploy/mender-demo --apply
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

可以实际测试 Workspace、授权连接、Catalog／Toolset、Run／Usage、Publisher，以及 Admin 的发布审核、发布历史、策略、执行治理、平台运营、JIT 和 sandbox 账务页面。

本地 OIDC 只证明 Mender 会话、PKCE／nonce／JWKS 处理和角色分离可工作；不能替代真实企业 IdP 验收。sandbox 支付不能被描述为真实充值。没有创建真实 Tool／Connection 时，执行类页面为空是正常的，不代表运行时会自动调用外部网络。

## 清理

普通停止不要删卷。如果明确决定删除这个测试实例，先备份需要的数据并人工核对 `mender_demo` 的 ownership 标记，再使用该实例自己的 Compose 文件处理；不要运行全局 `docker system prune` 或删除其他项目资源。

