# commerce · 计量与资金

状态：候选上下文骨架，G0 待冻结，尚无业务实现。来源：BC06、设计 G02。

- 所有权：PriceVersion、Wallet、Budget、Reservation、Settlement
- 职责：计价、预留、使用事实、分录和对账
- 计划负责角色：BE-A（尚未指派实际负责人）
- 候选公开合同：PriceQuote、ReservationReceipt、SettlementResult
- 边界：共享事务不开放任意跨域写表

目录依赖：`adapters → application → domain`；`public` 仅发布稳定合同。跨上下文依赖由出站防腐层访问他域 public，bootstrap 显式装配。
