import { Link } from 'react-router';
import { Button } from '@mender/ui';

export function HomePage() {
  return <>
    <p className="eyebrow">Mender / Admin</p>
    <h1>平台管理，从这里开始</h1>
    <p className="lead">在独立管理入口中配置版本化发布与执行策略、审核 Catalog / Toolset 发布请求，并追踪 append-only 治理历史与服务端执行风险事实。</p>
    <section className="welcome-panel" aria-labelledby="welcome-heading">
      <div><h2 id="welcome-heading">Maker / Checker 发布治理</h2><p>Publication review 与 history 都复用 Human OIDC session，但 reviewer / audit authority 由后端 Workspace membership 与独立 Governance DB role 重新裁决。</p></div>
      <div className="credential-actions"><Button asChild><Link to="/publication-reviews">打开发布审核</Link></Button><Button asChild variant="outline"><Link to="/publication-policy">管理发布策略</Link></Button><Button asChild variant="outline"><Link to="/publication-history">查看审计历史</Link></Button></div>
    </section>
    <section className="welcome-panel" aria-labelledby="execution-governance-heading">
      <div><h2 id="execution-governance-heading">Execution Governance</h2><p>管理不可变的执行策略 revision，并观察服务端 risk / outcome / reason、Human confirmation 与 exact arguments hash；浏览器不自行裁决执行权限或风险。</p></div>
      <div className="credential-actions"><Button asChild><Link to="/execution-governance">打开执行治理</Link></Button></div>
    </section>
    <section aria-label="后续能力规划">
      <p className="section-label">后续能力规划</p>
        <div className="capability-row">
          <span className="row-number">01</span>
          <div><h3>工作空间与供给</h3><p>管理工作空间、供应商与能力来源。</p></div>
          <span className="planned">计划接入</span>
        </div>
        <div className="capability-row">
          <span className="row-number">02</span>
          <div><h3>审核与发布</h3><p>对精确 revision 进行 maker/checker 审核；真正发布继续服务端重校验。</p></div>
          <span className="planned">Alpha 已接入</span>
        </div>
        <div className="capability-row">
          <span className="row-number">03</span>
          <div><h3>策略与治理审计</h3><p>版本化声明式发布策略产生服务端风险决议；approval、revision drift、publish consume/commit 与 retire 保留不可变时间线。</p></div>
          <span className="planned">Alpha 已接入</span>
        </div>
        <div className="capability-row">
          <span className="row-number">04</span>
          <div><h3>执行治理</h3><p>版本化 ExecutionPolicyRevision、Machine 风险上限、Human confirmation TTL 与执行决议均由服务端强制和投影。</p></div>
          <span className="planned">Alpha 已接入</span>
        </div>
    </section>
  </>;
}
