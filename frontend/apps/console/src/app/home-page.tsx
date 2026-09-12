import { Link } from 'react-router';
import { Button } from '@mender/ui';

export function HomePage() {
  return <>
    <p className="eyebrow">Mender / Console</p>
    <h1>欢迎使用 Mender</h1>
    <p className="lead">将 API、MCP 与 Agent 汇聚为可发现、可治理、可分发的能力。当前 Alpha 已开放受保护 Run 的查询与取消工作流。</p>
    <section className="welcome-panel" aria-labelledby="welcome-heading">
      <div><h2 id="welcome-heading">纵向 Alpha 已进入可观察阶段</h2><p>通过现有机器凭据读取 Run、事件与 Artifact 元数据；凭据只保留在当前页面内存。</p></div>
      <Button asChild><Link to="/runs">打开 Run Explorer <span aria-hidden="true">↗</span></Link></Button>
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
