# 原部署任务恢复：入口验证、CI 接线与真实复跑

继续原 S4-12／16／17；不是新的产品阶段。保留 `394599a` 的部署实现及未提交修正，没有覆盖用户改动或改写历史测试结果。当前可操作入口见[持久化部署指南](production-deployment.md)。

## 本轮完成

- 保留并验证 Console 认证代理、Admin 独立回调、文件秘密和受限角色初始化、固定 Linux 镜像、双域 TLS 与持久卷。
- 完成真实发布入口检查：`up --apply` 必须经宿主机访问两个 HTTPS `/readyz` 并得到正确服务身份后才成功；容器 healthy 不再足以声明部署成功。
- 探测有时间和响应大小上限，不带凭据、不跟随重定向、不调用业务工具。证书错误不会通过关闭 TLS 校验绕过。
- 校验声明的 origin 端口等于实际 TLS 发布端口；构建源码指纹覆盖前端编译前后；无副作用 `--help` 可直接使用。
- 修复 `.github/workflows/ci.yml` 遗留的 `pnpm deploy build`，改为 `pnpm run deploy build`。增加 CI 入口断言，避免调用 pnpm 同名内建命令而不执行项目脚本。

## 当前验证，而非复用中断记录

[实际复跑记录](verification/deployment-resume-20260917/verification.json)与[结构化部署结果](verification/deployment-resume-20260917/stack-results.json)记录了 11 项部署合同测试、165 项 Node 全量测试及 13 项真实部署步骤；所有本次执行均成功，完整检查还包括 Go、架构、Lint、类型、Vet 与双前端构建。Go 部分命中缓存。

新制品为 `dist/releases/production-resume-20260917`，运行镜像和 Web 镜像身份及测试时源码指纹写入记录。此目录不覆盖旧 release，也不表示镜像已推送到远端。完整部署命令耗时 74.319 秒。

真实负向测试覆盖错误服务身份、HTTP 重定向、超大响应、超时与不可信证书；正向链路经过 Nginx TLS、真实 PostgreSQL、签名 OIDC/JWKS/PKCE/nonce 交互和 Chromium，而不是注入登录 Cookie。另验证跨域 Cookie 隔离、API／数据库重启持久性、备份可读、停止后 release 切换、nonce 错误拒绝与退出失效。

桌面 1440×960 的 Console／Admin 及 390×844 移动退出页截图已查看；Browser 插件未提供，使用已有 Playwright。截图在 `.tmp/deploy-fixture-ebd01d21f2/`，不提交私有运行配置。

测试完成后单独查询了该 Compose project 的容器与卷，均为零。没有删除其他实例，未留下常驻生产服务。期间一次记录用子进程命令被工作区路径政策在启动前拒绝，未写文件；改用允许的文件工具整理观察记录，不改变测试结果。

## 仍未得到的批准和范围

这里的 IdP 是隔离协议模拟器，不是外部身份供应商认证；浏览器只在 fixture context 中接受私有证书，API 到 IdP 与 PostgreSQL 的证书校验保持启用，没有修改系统信任库。

本次没有重新执行独立 S4 数据库恢复套件或网络依赖扫描，既有记录仍按原日期保留。本次备份步骤证明实际数据库 dump 可读取和配置／秘密保持，不等于完整生产灾备、跨版本降迁移或角色／主密钥恢复。

Worker 仍默认关闭供应商执行，真实工具、Connection、价格、预算和出口配置需逐项提供并审核。镜像 OS 漏洞扫描、真实主机网络与告警、外部用户／商户／供应商材料和 G4／G5 决议未被本地自动化替代。未 push、未远端部署、未改分支保护、未调用真实付费服务。
