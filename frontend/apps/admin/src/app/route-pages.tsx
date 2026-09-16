import { NavLink, Outlet, Link } from 'react-router';
import { Shell, Button } from '@mender/ui';

export function Layout() {
  return <Shell label="Admin" navigation={<>
    <NavLink className="nav-link" to="/" end>开始</NavLink>
    <NavLink className="nav-link" to="/publication-reviews">发布审核</NavLink>
    <NavLink className="nav-link" to="/publication-history">审计历史</NavLink>
    <NavLink className="nav-link" to="/publication-policy">发布策略</NavLink>
    <NavLink className="nav-link" to="/execution-governance">执行治理</NavLink>
    <NavLink className="nav-link" to="/release-management">插件审核与发布</NavLink>
    <NavLink className="nav-link" to="/platform-operations">平台运营与异常</NavLink>
    <NavLink className="nav-link" to="/support-approvals">危险审批与临时支持</NavLink>
    <NavLink className="nav-link" to="/billing">账单与模拟支付</NavLink>
    <NavLink className="nav-link" to="/status">服务状态</NavLink>
  </>}><Outlet /></Shell>;
}

export function NotFound() {
  return <><p className="eyebrow">404</p><h1>页面不存在</h1>
    <p className="lead">这个地址尚未开放，或已经发生变化。</p>
    <Button asChild><Link to="/">返回首页</Link></Button></>;
}

export function RouteError() {
  return <main><h1>页面暂时无法打开</h1><p className="lead">请重新加载页面后再试。</p>
    <Button asChild><a href="/">返回首页</a></Button></main>;
}
