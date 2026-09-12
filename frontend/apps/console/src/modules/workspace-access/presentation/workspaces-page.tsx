import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router';
import { Button } from '@mender/ui';
import { ConsoleUnauthenticatedError, type WorkspaceGateway } from '../application/workspace-gateway';

const roleLabel = { owner: 'Owner', admin: 'Admin', developer: 'Developer', viewer: 'Viewer' } as const;

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Workspace access is temporarily unavailable.';
}

export function WorkspacesPage({ gateway }: { gateway: WorkspaceGateway }) {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['console-identity'], queryFn: ({ signal }) => gateway.load(signal), retry: false });
  const logout = useMutation({
    mutationFn: () => gateway.logout(),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: ['console-identity'] });
    },
  });

  if (query.error instanceof ConsoleUnauthenticatedError) return <>
    <p className="eyebrow">Mender / Workspaces</p><h1>登录 Console</h1>
    <p className="lead">使用团队配置的 OpenID Connect 身份登录。Mender 不把长期 IdP token 暴露给浏览器业务代码。</p>
    <Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button>
  </>;

  return <>
    <p className="eyebrow">Mender / Workspaces</p>
    <div className="page-heading-row"><div><h1>工作空间</h1><p className="lead">成员关系由服务端每次重新校验；前端选择不会授予额外权限。</p></div>{query.data && <Button type="button" variant="outline" disabled={logout.isPending} onClick={() => logout.mutate()}>{logout.isPending ? '正在退出…' : '退出登录'}</Button>}</div>
    {query.isPending && <div className="empty-state"><strong>正在读取账户与 Workspace…</strong></div>}
    {query.error && !(query.error instanceof ConsoleUnauthenticatedError) && <div className="error-panel" role="alert">{errorMessage(query.error)}</div>}
    {logout.error && <div className="error-panel" role="alert">{errorMessage(logout.error)}</div>}
    {query.data && <section className="workspace-access-panel" aria-labelledby="workspace-heading">
      <div className="workspace-user"><span>当前用户</span><strong className="mono">{query.data.userId}</strong></div>
      <div className="panel-heading"><div><p className="section-kicker">Memberships</p><h2 id="workspace-heading">可访问 Workspace</h2></div><span>{query.data.workspaces.length} 个</span></div>
      {query.data.workspaces.length === 0 ? <div className="empty-state"><strong>没有有效 Workspace 成员关系</strong><span>请联系 Workspace Owner 或平台管理员。</span></div> : <div className="workspace-card-grid">{query.data.workspaces.map((workspace) => <article className="workspace-card" key={workspace.id}>
        <div><p className="section-kicker">{roleLabel[workspace.role]}</p><h3 className="mono">{workspace.id}</h3></div>
        <div className="workspace-card-actions"><Button asChild variant="outline"><Link to={`/runs?workspace=${encodeURIComponent(workspace.id)}`}>打开 Runs</Link></Button></div>
      </article>)}</div>}
    </section>}
  </>;
}
