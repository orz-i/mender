# Mender 持久化部署与生产化交付

本轮继续原 S4-12／16／17，不新增阶段，不自动批准 G4 或 G5。生产服务器、域名、真实 OIDC、供应商和商户资格由操作者提供；没有这些事实时只能交付并验证部署能力，不能声称已经生产上线。

最近一次恢复验证见[入口校验与部署复跑](2026-09-17-deployment-resume.md)：实际镜像／TLS／登录／持久重启与备份切换已复测，测试实例已清理；不是一个仍在运行的默认演示站点。

## 架构与信任边界

使用单主机 Docker Compose。PostgreSQL 保存在命名卷，**不发布数据库端口**；跨容器数据库连接使用 `verify-full` 和部署私有 CA，而不是因为在 Docker 网段就关闭 TLS。Console 与 Admin 各自有 API 进程、独立 HTTPS 主机名和 OIDC 回调；共享持久身份库，不共享浏览器跨域 Cookie。

Web 镜像包含已经构建的两套 SPA，通过非特权 Nginx 端口提供 TLS、同源 `/auth`／`api`／`mcp` 代理与 SPA fallback。代理不重试业务请求、不记录 API／认证查询串、不缓冲 MCP 响应。没有实现的 SSE 或协议扩展不因代理支持流式传输而被宣称完成。

API／Worker 镜像基于 scratch，包含固定工具链编译的 Linux/amd64 二进制和可信 CA 根，不含 shell、Node、编译器或源代码秘密。运行服务只读文件系统、丢弃 capabilities、禁止提权、限制进程／内存和日志。数据库仍采用官方镜像入口初始化并降为 postgres 用户，不能误写为所有初始化步骤都以应用 UID 运行。

只有显式 operator／provision 服务能挂载管理数据库凭据；API、Worker、Web 各自只挂载所需秘密。原数据库和领域权限校验没有放宽。`*_DATABASE_URL_FILE` 与 `MENDER_CURSOR_SIGNING_KEY_FILE` 是新增文件输入；原值与文件同时存在会失败，不静默选择。Go 不自动读取 `.env`。

## 1. 准备构建机

使用仓库固定 Node 24.19.0、pnpm 11.18.0、Go 1.26.7 以及本地 Linux Docker Engine／Compose。构建机需要安装依赖；生产运行容器不需要 Go／Node。

本指南统一使用 **`pnpm run deploy`** 显式调用仓库脚本，不依赖与 pnpm 内建命令同名的快捷解析。`pnpm run deploy --help` 只输出用法，不读取秘密、不连接 Docker、不创建资源。构建前后核对源码指纹，覆盖前端编译和 Go 编译期间的变化。

```powershell
Set-Location D:\mender
pnpm install --frozen-lockfile
pnpm check:toolchain
docker pull postgres:18.6
docker pull nginx:1.30.5-alpine
pnpm run deploy build dist/releases/my-release
```

发布目录必须是新的，命令不覆盖旧制品。`release.json` 保存构建时 HEAD、完整源码指纹、每个构建文件的大小／SHA-256 与镜像 ID；镜像基础依赖固定为实际解析的 digest。工作区有未提交实现时使用文件指纹诚实标识，不冒充未来提交已被构建。

当前支持 Linux/amd64，不自动宣称 arm64 或多架构。发布目录只是 build output；通过构建不等于通过完整测试、漏洞评审或正式产品验收。

## 2. 准备持久本地配置

```powershell
pnpm run deploy prepare deploy/local.example.json dist/releases/my-release .local/deploy/local
pnpm run deploy preflight .local/deploy/local
pnpm run deploy up .local/deploy/local --apply
pnpm run deploy status .local/deploy/local
```

`prepare` 只写入新的忽略目录：随机独立角色密码、DSN 文件、游标／flow 签名秘密、私有数据库 CA、Compose 与 Web 配置。不会自动创建用户、批准版本、充值或选择供应商。再次对相同目录 prepare 会拒绝；不能用重新生成秘密来修复已有数据库密码不匹配。

`up --apply` 是显式副作用：先启动自有实例数据库，再运行当前镜像的 migration／限定角色 provisioning，最后启动 API、Worker 和 Web。已有角色只在同实例标记、非高权及凭据一致时复用；不接管他人角色，不自动重设密码。原 `operator` 命令仍可单独使用。

启动成功还要求从宿主机访问两个实际发布的 HTTPS `/readyz`，同时返回 Mender API 的正确就绪身份；容器内部 healthy 不再独自构成成功条件。检查最多等待60秒，只重试无身份、无副作用的 GET，不跟随重定向，也不重试业务调用。local 模式把该探测连接固定到回环并显式信任本实例私有 CA；production 使用真实域名和正常证书验证。使用自带 Web 证书的 local 实例须具备对应宿主信任，不能依靠关闭 TLS 校验通过。

若 `up` 在入口检查失败，命令返回非零，但会保留实例供操作者诊断或执行 `stop`；不会自动删除持久卷或恢复旧数据库。此时不能把部分启动当成部署已完成。

本地示例访问 `https://console.localhost:18443` 和 `https://admin.localhost:18443`。默认使用私有测试证书，不改变 OS／浏览器信任库；操作者需正确配置本地证书信任或提供自己的证书。**这个示例没有 OIDC，因此不能登录；它用于持久服务启动验证，不是假装有默认管理员的完整演示。**

`pnpm run deploy stop .local/deploy/local` 停止进程但保留卷。不要使用其他项目的容器名，也不要执行不加实例限制的 prune 或 down --volumes。部署生成目录中的 CA 私钥、密码和配置需备份并限制 ACL，不能上传 Git。

## 3. 配置真实 OIDC 和两个入口

复制示例到忽略的本地 JSON，再增加 HTTPS issuer、client ID 和客户端秘密文件路径。客户端允许两个精确回调：

```text
https://<console-host>/auth/callback
https://<admin-host>/auth/callback
```

两个主机名必须不同，不只是端口不同。各 API 进程选择自己的回调和 flow key，Cookie 为 host-only、Secure、HttpOnly。Admin 的固定 `/workspaces` 登录落点映射到其首页，不引入任意返回 URL。Console Vite 的 `/auth` 代理也已补齐。

真实 issuer 必须 HTTPS；如使用内部 CA，可显式提供 `oidc_ca_file` 并由部署挂载为 Go 的可信 CA 输入。该配置不会关闭证书校验。`local_oidc_host_gateway` 只允许 local 模式的 `idp.localhost` 集成 fixture，production 拒绝该开关。

本轮自动验证使用一个隔离的、真实签名并验证 code／PKCE／nonce／JWKS 的协议模拟 IdP，不是外部 IdP 认证。它只存在于测试脚本，永不进入生产镜像或默认部署。

## 4. 通过操作员建立真实用户

```powershell
pnpm run deploy operator .local/deploy/local provision-human --user user_maker --display-name Maker --issuer https://YOUR_ISSUER --oidc-subject REAL_SUB --workspace ws_local --membership-role owner
pnpm run deploy operator .local/deploy/local provision-platform-staff --user user_maker --platform-role operator
```

为另一个真实主体建立 checker 和 reviewer 角色。Workspace owner 不等于平台人员，自审保护不因为“只是部署”而关闭。issuer／sub 必须来自 IdP，而非自行猜测邮箱或把机器 Key 粘进浏览器。

`issue-key` 显式操作会仅在成功持久化后输出一次机器秘密；请安全保存，不收集到公共日志。本指南不提供默认密码或可复用测试 session。

## 5. 生产配置与制品批准

`deploy/production.example.json` 是故意不能直接通过的模板。必须换成真实域名、HTTPS issuer、client ID、受保护秘密文件及覆盖 Console／Admin 的有效完整证书链和私钥。证书至少剩余七天有效期，公私钥必须匹配。CA 签名与域名拥有权仍由真实证书流程保证，文件可解析不等于受信任。

在忽略目录保存已填写的配置，例如 `.local/operator/production.json`。两个 origin 的显式或默认 TLS 端口必须与 `https_port` 一致，否则在创建部署前拒绝；不能把 443 的回调地址配给只发布 18443 的服务。先准备一个新实例目录，再验签预检：

```powershell
pnpm run deploy prepare .local/operator/production.json dist/releases/my-release .local/deploy/production
```

生产主机需要可用的同一制品目录、对应精确镜像及部署工具；本轮不会自动 SSH、推送镜像、修改 DNS 或复制秘密。应先在经批准的目标主机建立这些输入，再执行下面的 preflight／up。不能将本机已有镜像 ID 误当作目标服务器已经装载该镜像。

生产模式只暴露 80／443 到主机，容器仍监听 8080／8443；数据库和 API 不直接公开。Admin 还应由实际网络策略限制到 VPN／允许的运营来源；这不是源码可以替你签署的网络事实。

生产 preflight／up 要求来自独立信任配置的公钥和对本次 `release.json` 的有效 Ed25519 签名。沿用已有工具：

```powershell
pnpm release:artifact sign dist/releases/my-release/signing-inventory.json .local/operator/release-private.key
# 将输出安全保存为显式选择的 envelope JSON；命令不发布或安装密钥。
pnpm run deploy preflight .local/deploy/production --signed .local/operator/release-envelope.json --trusted-key .local/operator/release-public.pem
pnpm run deploy up .local/deploy/production --apply --signed .local/operator/release-envelope.json --trusted-key .local/operator/release-public.pem
```

签名覆盖记录所有制品字节和镜像身份的 manifest，preflight 再核对本地制品、镜像 ID、服务硬化和 secret 归属。独立可信公钥不能来自待验制品自身；签名也不替代供应商许可、漏洞评审或 G4／G5 人工批准。

生产 host 的 Docker 管理权与部署配置目录必须严格限制；能修改 Docker daemon、受信公钥或部署脚本的人属于控制平面信任边界。文件型 Compose secrets 不是加密保险库，仍依赖主机 ACL 和备份保护。

## 6. Worker 与首个真实业务能力

默认 Worker 仅启用独立控制角色，**不自动打开供应商网络执行**，默认 Workspace scope 为 `ws_local`。这避免把新的持久部署误变成自动付费调用。实际 Workspace、部署 revision、executor/reconciler/settlement、秘密挂载和精确出口需要按照现有 reviewed runtime 合同审核配置。

数据库迁移和角色初始化不制造 ToolVersion、Connection、价格、预算、发布审批或供应商凭据。真实业务入口应使用现有管理用例建立并审核；首次执行需要所有版本／授权／预算事实齐全。生产化基础设施交付不等于这些业务资料已提供。

## 7. 回退、备份与运维

本轮增加显式受保护备份和停止后 release 切换，不提供自动降迁移：

```powershell
pnpm run deploy backup .local/deploy/local .local/backups/before-update --apply
pnpm run deploy stop .local/deploy/local
pnpm run deploy switch-release .local/deploy/local dist/releases/reviewed-next --apply --migrations-reviewed
pnpm run deploy preflight .local/deploy/local
pnpm run deploy up .local/deploy/local --apply
```

production 的 switch-release 和 up 同样需要目标 release 的签名 envelope 与独立可信公钥。切换只更新已审制品身份并保存上一份 Compose／deployment 元数据，不覆盖密码、证书或卷，也不自动启动应用。运行中切换被拒绝；数据库镜像改变不走该入口。

备份为真实 `pg_dump --format=custom --no-owner --no-acl` 文件并保存 SHA-256 与来源记录；它明确不包含角色密码、ACL、秘密目录、CA 私钥、对象文件或外部副作用。恢复需要独立受审流程和这些依赖，不能仅凭 pg_restore 可读就声称生产恢复完成。

生成实例使用随机 ownership 标记绑定 Compose 服务与持久卷。相同 project 名字下发现不匹配资源即停止，不能接管用户已存在的容器。不要手工改写生成 Compose 来绕过签名、秘密或镜像检查；配置改变应形成新的审阅记录。

保留上一个经过验证的 release、镜像 ID、配置和独立数据库备份。应用回退不得自动 down migration、覆盖账本或删除卷；迁移不兼容时先停止新受理，再按已批准恢复流程核对外部副作用。恢复后不能盲目启动 Worker 重发未知任务。

已有[备份恢复手册](2026-09-16-s4-operations-runbook.md)继续适用，但生产还要覆盖角色／ACL、秘密及密钥、对象文件和实际保留周期。当前 Compose 是单主机拓扑，不包含高可用数据库、云 KMS、监控平台或生产容量保证。

密钥更新需要受控重启。改变证书、角色密码或 OIDC secret 之前，应先更新真实上游或数据库，再同步文件和运行服务；不能仅改文件指望已打开连接自动安全轮换。

## 8. 验证与证据边界

```powershell
pnpm test:deployment:contract
pnpm test:deployment dist/releases/my-release
pnpm check
pnpm test:s4:recovery
```

部署测试使用随机 `mender_test_*` 实例、真正的 Compose 镜像、Nginx TLS、受限 PostgreSQL 角色、HTTPS OIDC/JWKS 与 Chromium；测试结束只清理该实例的自有容器和卷，不清理用户的持久实例。截图和运行结果在 `.tmp/deploy-fixture-*/`，私有素材留在忽略目录。

本轮测试的 Browser 插件未提供，使用仓库已有 Playwright。对本地 fixture 证书的浏览器例外只用于测试，API 对 IdP 和数据库仍校验证书，未修改系统信任库。实际测试结果与剩余项以单独工程记录和命令回执为准，本文不预填通过。

## 官方参考

- Docker Compose secrets：https://docs.docker.com/compose/how-tos/use-secrets/
- Compose 就绪依赖：https://docs.docker.com/compose/how-tos/startup-order/
- PostgreSQL 官方镜像与18版卷位置：https://hub.docker.com/_/postgres
- Nginx 官方镜像与固定版本：https://hub.docker.com/_/nginx
- Docker 构建与制品建议：https://docs.docker.com/build/building/best-practices/
- pnpm 显式运行同名脚本：https://pnpm.io/cli/run

上述来源用于接口与部署机制核验，不是对 Mender 生产环境的认证。
