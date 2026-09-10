# 内部原子受理协调

ADR-020 的正式 G0 批准仍待完成。本目录已有消费方端口、用例顺序、精确输入编解码、防腐层和事务适配器；不直接写 execution／commerce 的表。bootstrap 在同一事务中装配两个上下文自有服务，使额度预留、Run、受理快照、blocked Job 和 Outbox 原子提交。

真实授权／Catalog 计划解析尚未装入 API；没有公共 StartRun。只在隔离测试资源验证内部入口，详细边界与证据见 [本轮实现记录](../../../../docs/engineering/2026-09-09-atomic-admission.md)。
