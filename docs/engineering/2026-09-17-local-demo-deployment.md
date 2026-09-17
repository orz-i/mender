# Mender 本地持久测试实例实际部署记录

日期：2026-09-17。目标是给开发者留下一个**持续运行、可实际登录、可重启保留数据**的本地 Docker 实例；不连接真实支付、真实供应商或远端生产。

## 实际实例

- Compose project：`mender_demo`
- 部署目录：`.local/deploy/mender-demo`（Git 忽略，包含本实例秘密与私有 CA）
- Release：`dist/releases/local-use-20260917`
- Console：`https://console.localhost:18443`
- Admin：`https://admin.localhost:18443`
- 本地测试 OIDC：`https://idp.localhost:19443`
- Workspace：`ws_local`
- Console 测试身份：`user_maker` / OIDC subject `maker` / Workspace owner / Platform operator
- Admin 测试身份：`user_checker` / OIDC subject `checker` / Workspace owner / Platform reviewer

本地 OIDC 是 Mender 明确的 local-only fixture。配置中必须 `environment=local`、issuer 必须为 `idp.localhost`，production 模式拒绝该配置。client secret 在 prepare 时生成并只保存在 `.local` 部署秘密目录，不进入 Git。

## 实际验证

1. `pnpm test:deployment:contract`：12 项 Node 部署合同全部通过，Go config／bootstrap／architecture 通过。
2. 完整 `pnpm check`：通过；当前工程、类型、Lint、Go 测试、Vet、前后端构建无阻断失败。
3. `pnpm run deploy preflight .local/deploy/mender-demo`：通过。
4. `pnpm run deploy up .local/deploy/mender-demo --apply`：通过；宿主 Console／Admin HTTPS readiness 均验证成功。
5. 本地 IdP 端口经修复后实际发布为 `127.0.0.1:19443 -> 19443/tcp`，不再只存在于 internal Compose 网络。
6. OIDC 协议链实际验证：Console 登录得到 `user_maker` + `ws_local`，Admin 登录得到 `user_checker` + `ws_local`；浏览器会话对应 session／CSRF Cookie 均由服务端签发。
7. 重启 PostgreSQL、Console/Admin API 和 local-idp 后再次执行登录／Workspace 查询仍通过，证明身份与 Workspace 持久在命名卷中。

首次协议验证暴露 local-idp 只接 internal network 时宿主端口无法 NAT 的问题。该实例是本轮新建、尚未交付使用的自有测试资源，因此只删除并重建 `mender_demo` 及自己的 `mender_demo_pgdata`，没有清理其他 Docker 资源。修复为 local-idp 同时接 `private` 和 `egress` 网络后重建，其他服务和数据库仍不对宿主暴露。

## 交付给使用者

实例保持运行。浏览器访问自签 HTTPS 前，需要自行信任 `.local/deploy/mender-demo/local-ca.crt`，或分别在本地测试浏览器中确认 Console／Admin／IdP 证书例外。不要把 `pki/ca.key`、数据库密码或生成的 secret 文件发送到聊天或提交仓库。

日常状态、停止和再次启动：

```powershell
pnpm run deploy status .local/deploy/mender-demo
pnpm run deploy stop .local/deploy/mender-demo
pnpm run deploy up .local/deploy/mender-demo --apply
```

停止保留数据库卷和身份数据；不要再次执行 `prepare`。完整说明见 [local-demo.md](local-demo.md)。

## 边界

当前数据库没有自动创建真实 Provider、ToolVersion、Connection、供应商凭据或真实预算资金。Worker 外部 dispatch 继续 fail closed。用户可以实际测试 Workspace、Catalog／Connection／Publisher、Run／Usage 及 Admin 工作台，但需要按产品流程创建相应业务事实后，才会出现可执行工具。

本记录不批准 G4／G5，不证明真实企业 IdP、真实 PSP、供应商许可或生产值班已经完成。
