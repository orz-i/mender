import { Link } from 'react-router';
import { Button } from '@mender/ui';

export function HomePage() {
  return <>
    <p className="eyebrow">Mender / Admin</p>
    <h1>平台管理，从这里开始</h1>
    <p className="lead">在独立管理入口中审核 Catalog / Toolset 发布请求，并追踪平台运行。</p>
    <section className="welcome-panel" aria-labelledby="welcome-heading">
      <div><h2 id="welcome-heading">Maker / Checker 发布治理</h2><p>Publication review 复用 Human OIDC session，但 reviewer authority 由后端 Workspace membership 与独立 Governance DB role 重新裁决。</p></div>
      <Button asChild><Link to="/publication-reviews">打开发布审核 <span aria-hidden="true">↗</span></Link></Button>
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
          <div><h3>审计与额度事实</h3><p>后续连接审核记录、执行事实与 quota/allowance 可观测信息。</p></div>
          <span className="planned">计划接入</span>
        </div>
    </section>
  </>;
}
