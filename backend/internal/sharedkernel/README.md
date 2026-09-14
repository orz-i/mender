# Shared Kernel

仅在多个上下文确实共享稳定语义时加入最小纯类型；不加入通用实体、仓储或服务定位器。

当前仅包含 `canonicaljson`：它定义 Governance Human confirmation 与 Admission 共享的 bounded canonical JSON object identity 规则。SHA-256 仍由各 adapter 本地执行，Shared Kernel 不持有认证、策略、quota 或执行语义。
