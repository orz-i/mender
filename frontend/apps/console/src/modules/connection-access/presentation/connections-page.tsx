import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button } from '@mender/ui';
import { ConnectionLoginRequiredError, type ConnectionGateway } from '../application/connection-gateway';
import { canManageConnections, type ConnectionState } from '../domain/connection';

const stateLabel: Record<ConnectionState, string> = { active: '可用', expired: '已过期', revoked: '已撤销', error: '异常' };
function formatTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }
function message(error: unknown) { return error instanceof Error ? error.message : 'Connection operation failed.'; }

export function ConnectionsPage({ gateway }: { gateway: ConnectionGateway }) {
  const queryClient = useQueryClient();
  const [selection, setSelection] = useState('');
  const workspaces = useQuery({ queryKey: ['connection-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const selected = selection || workspaces.data?.[0]?.id || '';
  const selectedMembership = workspaces.data?.find((item) => item.id === selected);
  const connections = useQuery({ queryKey: ['console-connections', selected], enabled: selected !== '', queryFn: ({ signal }) => gateway.list(selected, signal), retry: false });
  const revoke = useMutation({
    mutationFn: (connectionId: string) => gateway.revoke(selected, connectionId),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['console-connections', selected] }); },
  });

  if (workspaces.error instanceof ConnectionLoginRequiredError || connections.error instanceof ConnectionLoginRequiredError) return <>
    <p className="eyebrow">Mender / Connections</p><h1>登录 Console</h1><p className="lead">Connection 自助管理使用受保护的人类会话，不接受页面手填的长期 IdP token。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button>
  </>;

  return <>
    <p className="eyebrow">Mender / Connections</p><h1>授权连接</h1><p className="lead">这里仅展示安全元数据。Credential Version、Secret 和 Connection Grant 不会返回浏览器。</p>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length === 0 && <div className="empty-state"><strong>没有可访问 Workspace</strong></div>}
    {workspaces.data && workspaces.data.length > 0 && <section className="run-list-panel" aria-label="Connection 列表">
      <div className="panel-heading"><div><p className="section-kicker">Connections</p><h2>Workspace Connections</h2></div><div className="run-controls"><label>Workspace<select value={selected} onChange={(event) => setSelection(event.target.value)}>{workspaces.data.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.id} · {workspace.role}</option>)}</select></label><Button type="button" variant="outline" disabled={connections.isFetching} onClick={() => void connections.refetch()}>刷新</Button></div></div>
      {connections.isPending && <div className="empty-state"><strong>正在读取 Connection…</strong></div>}
      {connections.error && !(connections.error instanceof ConnectionLoginRequiredError) && <div className="error-panel" role="alert">{message(connections.error)}</div>}
      {connections.data && connections.data.length === 0 && <div className="empty-state"><strong>当前 Workspace 没有 Connection</strong><span>OAuth/BYOK 创建流程将在后续切片接入。</span></div>}
      {connections.data && connections.data.length > 0 && <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Connection</th><th>Provider</th><th>状态</th><th>到期</th><th>操作</th></tr></thead><tbody>{connections.data.map((item) => <tr key={item.id}><td className="mono">{item.id}</td><td className="mono">{item.providerId}</td><td><span className={`state-badge state-${item.state}`}>{stateLabel[item.state]}</span></td><td>{formatTime(item.expiresAt)}</td><td>{canManageConnections(selectedMembership?.role ?? 'viewer') && item.state !== 'revoked' ? <Button type="button" variant="outline" disabled={revoke.isPending} onClick={() => revoke.mutate(item.id)}>撤销</Button> : <span className="muted-copy">只读</span>}</td></tr>)}</tbody></table></div>}
      {revoke.error && <div className="error-panel" role="alert">{message(revoke.error)}</div>}
    </section>}
  </>;
}
