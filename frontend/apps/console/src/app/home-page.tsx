import { Link } from 'react-router';
import { Button } from '@mender/ui';

export function HomePage() {
  return <>
    <p className="eyebrow">Mender / Console</p>
    <h1>欢迎使用 Mender</h1>
    <p className="lead">将 API、MCP 与 Agent 汇聚为可发现、可治理、可分发的能力。</p>
    <section className="welcome-panel" aria-labelledby="welcome-heading">
      <div><h2 id="welcome-heading">你的能力工作空间</h2><p>当前为初始化预览，工作空间与工具接入功能将在后续版本开放。</p></div>
      <Button asChild><Link to="/status">检查服务连接 <span aria-hidden="true">↗</span></Link></Button>
    </section>
    <section aria-label="后续能力规划">
      <p className="section-label">后续能力规划</p>
        <div className="capability-row">
          <span className="row-number">01</span>
          <div><h3>HTTP API</h3><p>将已有接口转换为统一工具。</p></div>
          <span className="planned">计划接入</span>
        </div>
        <div className="capability-row">
          <span className="row-number">02</span>
          <div><h3>MCP Tools</h3><p>连接远程工具，并按工作空间分发。</p></div>
          <span className="planned">计划接入</span>
        </div>
        <div className="capability-row">
          <span className="row-number">03</span>
          <div><h3>Remote Agent</h3><p>通过统一的任务合同协作与执行。</p></div>
          <span className="planned">计划接入</span>
        </div>
    </section>
  </>;
}
