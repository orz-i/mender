import { NavLink, Outlet, Link } from 'react-router';
import { Shell, Button } from '@mender/ui';

export function Layout() {
  return <Shell label="Admin" navigation={<>
    <NavLink className="nav-link" to="/" end>开始</NavLink>
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
