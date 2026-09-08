import { Link } from 'react-router';
import { Button } from '@mender/ui';

export function HomePage() {
  return <>
    <p className="eyebrow">Mender / Admin</p>
    <h1>平台管理，从这里开始</h1>
    <p className="lead">在统一的管理入口中，组织供给、审核发布，并追踪平台运行。</p>
    <section className="welcome-panel" aria-labelledby="welcome-heading">
      <div><h2 id="welcome-heading">独立的平台管理入口</h2><p>当前为初始化预览，平台身份验证与管理操作尚未接入。</p></div>
      <Button asChild><Link to="/status">检查服务连接 <span aria-hidden="true">↗</span></Link></Button>
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
          <div><h3>审核与部署</h3><p>在发布前完成审核，保留可追溯的变更。</p></div>
          <span className="planned">计划接入</span>
        </div>
        <div className="capability-row">
          <span className="row-number">03</span>
          <div><h3>审计与对账</h3><p>连接操作记录、执行事实与费用明细。</p></div>
          <span className="planned">计划接入</span>
        </div>
    </section>
  </>;
}
