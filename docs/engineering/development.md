# Mender 本地开发

## 环境与安装

使用 Node.js `24.19.0`、pnpm `11.18.0` 和 Go `1.26.7`。仓库中的 `.node-version`、`packageManager`、`engines`、`backend/go.mod` 与 `toolchain.versions.json` 必须同步维护。版本不一致时检查会失败，不自动替换系统工具链。

在仓库根执行：

```sh
pnpm install --frozen-lockfile
pnpm check:toolchain
```

Go 首次构建会根据 `go.sum` 下载模块，也可先在 `backend/` 执行 `go mod download`。当前骨架不需要 Python、Docker、数据库或真实凭据。原合同包的 Python 校验脚本仅作为可选参考保留。

## 启动入口

每个长驻命令使用独立终端，Ctrl+C 停止：

| 命令（仓库根） | 服务 |
| --- | --- |
| `pnpm dev` | 同时启动 Console 5173 与 Admin 5174 |
| `pnpm dev:console` | 仅 Console |
| `pnpm dev:admin` | 仅 Admin |
| `pnpm dev:api` | API，默认绑定 `127.0.0.1:18080` |
| `pnpm dev:worker` | 空闲 Worker；尚无数据库或任务消费 |

Go 也可在 `backend/` 直接运行 `go run ./cmd/api` 或 `go run ./cmd/worker`。Console 与 Admin 的本地端口独立；这只是构建和开发入口隔离，平台身份认证仍待实现。

## 配置

API 读取进程环境变量，不自动读取 `.env`。默认无需配置。如需改变监听地址，PowerShell 中：

```powershell
$env:MENDER_HTTP_ADDR = '127.0.0.1:18081'
pnpm dev:api
```

在相应前端应用目录把 `.env.example` 复制为 `.env.local` 并修改 `MENDER_API_PROXY_TARGET`，或通过进程环境变量设置；修改后重启 Vite。默认代理目标为 `http://127.0.0.1:18080`，`/healthz`、`/readyz` 和预留的 `/api` 都通过开发服务器代理。该变量只供 Vite 服务端使用，不放入浏览器包。

`backend/.env.example` 是配置说明；前端两个应用的 `.env.example` 是 Vite 配置示例。真实本地配置已被 `.gitignore` 排除。

## 当前接口

| 路径 | 当前行为 |
| --- | --- |
| `GET /healthz` | `200`，`{"service":"mender-api","status":"ok","stage":"bootstrap"}` |
| `GET /readyz` | `503`，`not_ready`；业务依赖尚未装配 |
| 其他路径，包括合同设计中的 `/v1/runs` | `404`，尚未实现 |

前端 `/` 是初始化介绍，`/status` 发送真实健康请求；网络失败或响应不合法时显示恢复入口。未知前端路由提供 404 和返回首页。服务状态只确认 API 进程连通，不表示 Worker、认证、存储或业务链路健康。

## 构建与检查

```sh
pnpm check
```

需要缩小检查范围时，使用 [脚本说明](../../scripts/README.md)。`pnpm build` 产物分别位于 `frontend/apps/console/dist` 和 `frontend/apps/admin/dist`。共享包直接导出源码，由 Vite 构建，无需提前生成共享 JS 制品。

`pnpm build:backend` 编译所有 Go 包以验证；需要可执行文件时在 `backend/` 执行：

```sh
go build -o bin/ ./cmd/api ./cmd/worker
```

`pnpm --filter @mender/console preview` 和 Admin 的同名命令仅预览本地构建，默认端口为 6173／6174。部署时静态站点需要配置 SPA fallback 和同源 API 反向代理；仓库没有生产部署配置或授权流程。

## 常见情况

- `/readyz` 返回 503 是当前阶段的预期状态，不要用成功存活探针冒充业务就绪。
- 页面连接失败时先检查 API 终端、监听端口和代理目标，再点“重新检查”。
- 端口冲突会直接报错，不自动切到其他端口。修改配置或结束自己启动的旧进程。
- 工具链检查失败时安装清单指定版本；升级需一起调整配置、真实锁文件与验证证据。
- pnpm 拒绝过新的或未获准构建脚本的依赖时，检查具体依赖和发布元数据；不使用全量豁免作为默认安装方式。

CI 文件已创建但未在远程运行。原设计的完整架构、安全、资金和协议门禁见 G07，当前基础检查不能替代这些工作。
