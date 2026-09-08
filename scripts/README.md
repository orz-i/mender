# Mender 检查入口

均从仓库根使用 pnpm：

| 命令 | 验证范围 |
| --- | --- |
| `pnpm check` | 下列必要检查和构建的聚合入口，任一失败即停止 |
| `pnpm check:toolchain` | Node／pnpm／Go 精确版本与清单一致 |
| `pnpm check:workspace` | 必需包、scripts、workspace: 依赖、唯一锁文件 |
| `pnpm check:docs` | 原始 SHA-256、计划计数／引用、当前 Markdown 本地链接 |
| `pnpm check:contracts` | Schema、样例、制品摘要、OpenAPI 结构 |
| `pnpm lint` | JS／TS／React 语法和基础规则 |
| `pnpm typecheck` | 工作区严格 TypeScript 检查 |
| `pnpm test` | 健康传输成功、HTTP 失败、非法响应和取消 |
| `pnpm test:backend` | 探针语义、未开放业务路径和进程生命周期测试 |
| `pnpm vet:backend` | Go vet |
| `pnpm build:backend` | Go 全包编译 |
| `pnpm build` | Console／Admin 独立构建 |

脚本只校验其声明的范围，不将文本扫描当作完整依赖图、导入纯度或数据库所有权证明。原设计的 F01–F12／T41–T48 中尚未覆盖部分见 [初始化报告](../docs/engineering/initialization-report.md)。

`backend.mjs` 只使用无 shell 的子进程调用 Go，便于 Windows 与 Linux 共享命令；它不运行安装器，也不自动切换 Go 版本。
