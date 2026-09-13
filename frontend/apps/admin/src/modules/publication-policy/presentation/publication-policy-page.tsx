import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { PolicyLoginRequiredError, type PolicyGateway } from '../application/policy-gateway';
import type { PublicationPolicyInput, PublicationPolicyRevision, PublicationRiskLevel } from '../domain/policy';

const riskLevels: PublicationRiskLevel[] = ['low', 'medium', 'high', 'critical'];
const stateLabel = { draft: '草稿', active: '生效中', retired: '已退役' } as const;
function message(error: unknown) { return error instanceof Error ? error.message : 'Publication policy 操作失败。'; }
function formatTime(value: string | null) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }

export function PublicationPolicyPage({ gateway }: { gateway: PolicyGateway }) {
  const queryClient = useQueryClient(); const [params] = useSearchParams();
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const [form, setForm] = useState<PublicationPolicyInput>({ id: '', maxRiskLevel: 'high', denyUnsafeWrite: true, denyMcpUnsafeWrite: true });
  const workspaces = useQuery({ queryKey: ['admin-policy-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const snapshot = useQuery({ queryKey: ['admin-publication-policy', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.snapshot(workspaceId, signal), retry: false });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin-publication-policy', workspaceId] });
  const create = useMutation({ mutationFn: () => gateway.create(workspaceId, form), onSuccess: async () => { setForm((current) => ({ ...current, id: '' })); await refresh(); } });
  const activate = useMutation({ mutationFn: (id: string) => gateway.activate(workspaceId, id), onSuccess: refresh });
  const loginRequired = workspaces.error instanceof PolicyLoginRequiredError || snapshot.error instanceof PolicyLoginRequiredError;

  if (loginRequired) return <><p className="eyebrow">Mender / Admin / Governance</p><h1>登录 Admin</h1><p className="lead">Publication policy 使用受保护 Human OIDC session，策略管理权限由服务端 Workspace membership 与独立 Governance role 重新授权。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button></>;

  return <>
    <p className="eyebrow">Mender / Admin / Governance</p>
    <div className="page-heading-row"><div><h1>发布策略 / 风险规则</h1><p className="lead">创建不可变的声明式 PolicyRevision，并查看服务端对精确 target revision 产生的 PolicyDecision。页面不会根据 ToolVersion 属性自行计算风险或 allow / deny。</p></div></div>
    <div className="success-panel" role="note"><strong>Server-owned risk decision</strong><span>策略只包含风险上限与 unsafe-write 开关；没有脚本、SQL 或表达式入口。激活新 revision 会让已有 pending/approved publication approval 失效并重新走治理链。</span></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && !(workspaces.error instanceof PolicyLoginRequiredError) && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length > 0 && <>
      <section className="run-list-panel" aria-label="Publication policy workspace"><div className="panel-heading"><div><p className="section-kicker">Workspace</p><h2>Policy boundary</h2></div><div className="run-controls"><label>Workspace<select value={workspaceId} onChange={(event) => setSelection(event.target.value)}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label><Button type="button" variant="outline" disabled={snapshot.isFetching} onClick={() => void snapshot.refetch()}>刷新</Button></div></div></section>
      {snapshot.error && !(snapshot.error instanceof PolicyLoginRequiredError) && <div className="error-panel" role="alert">{message(snapshot.error)}</div>}
      {create.error && <div className="error-panel" role="alert">{message(create.error)}</div>}{activate.error && <div className="error-panel" role="alert">{message(activate.error)}</div>}
      <section className="launch-panel" aria-labelledby="policy-create-heading"><div className="panel-heading"><div><p className="section-kicker">Policy revision</p><h2 id="policy-create-heading">创建声明式策略草稿</h2></div></div><div className="launch-grid"><div className="launch-request"><label>Policy ID<input value={form.id} onChange={(event) => setForm({ ...form, id: event.target.value })} /></label><label>允许的最高风险<select value={form.maxRiskLevel} onChange={(event) => setForm({ ...form, maxRiskLevel: event.target.value as PublicationRiskLevel })}>{riskLevels.map((risk) => <option key={risk} value={risk}>{risk}</option>)}</select></label></div><div className="launch-request"><label><input type="checkbox" checked={form.denyUnsafeWrite} onChange={(event) => setForm({ ...form, denyUnsafeWrite: event.target.checked })} /> deny unsafe write</label><label><input type="checkbox" checked={form.denyMcpUnsafeWrite} onChange={(event) => setForm({ ...form, denyMcpUnsafeWrite: event.target.checked })} /> deny MCP-exposed unsafe write</label><Button type="button" disabled={create.isPending || form.id === ''} onClick={() => create.mutate()}>创建 PolicyRevision draft</Button></div></div></section>
      {snapshot.data && <><PolicyRevisionTable items={snapshot.data.revisions} busy={activate.isPending} activate={(id) => activate.mutate(id)} /><section className="run-list-panel" aria-label="Policy decisions"><div className="panel-heading"><div><p className="section-kicker">Decisions</p><h2>最近服务端风险决议</h2></div></div>{snapshot.data.decisions.length === 0 ? <div className="empty-state"><strong>尚无 PolicyDecision</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Target</th><th>Policy</th><th>风险 / Outcome</th><th>Reasons</th><th>时间</th></tr></thead><tbody>{snapshot.data.decisions.map((item) => <tr key={item.sequence}><td><strong>{item.targetKind === 'tool_version' ? 'ToolVersion' : 'Toolset'}</strong><div className="mono">{item.targetId}</div><div className="muted-copy">target revision {item.targetRevision}</div></td><td><span className="mono">{item.policyRevisionId}</span><div className="muted-copy">revision {item.policyRevision} · decision {item.sequence}</div></td><td><span className={`state-badge state-${item.outcome === 'allow' ? 'published' : 'retired'}`}>{item.outcome}</span><div className="muted-copy">risk {item.riskLevel}</div></td><td>{item.reasonCodes.map((code) => <div className="mono" key={code}>{code}</div>)}</td><td>{formatTime(item.evaluatedAt)}</td></tr>)}</tbody></table></div>}</section></>}
    </>}
  </>;
}

function PolicyRevisionTable({ items, busy, activate }: { items: PublicationPolicyRevision[]; busy: boolean; activate: (id: string) => void }) {
  return <section className="run-list-panel" aria-label="Policy revisions"><div className="panel-heading"><div><p className="section-kicker">Revisions</p><h2>不可变策略版本</h2></div></div>{items.length === 0 ? <div className="empty-state"><strong>当前 Workspace 尚无 PolicyRevision</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Policy</th><th>规则</th><th>状态</th><th>Lifecycle</th><th>操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong className="mono">{item.id}</strong><div className="muted-copy">revision {item.revision}</div></td><td><div>max risk: <strong>{item.maxRiskLevel}</strong></div><div className="muted-copy">deny unsafe: {String(item.denyUnsafeWrite)} · deny MCP unsafe: {String(item.denyMcpUnsafeWrite)}</div></td><td><span className={`state-badge state-${item.state === 'active' ? 'published' : item.state === 'retired' ? 'retired' : 'draft'}`}>{stateLabel[item.state]}</span></td><td><div>Created {formatTime(item.createdAt)}</div><div className="muted-copy">Activated {formatTime(item.activatedAt)} · Retired {formatTime(item.retiredAt)}</div></td><td>{item.state === 'draft' ? <Button type="button" disabled={busy} onClick={() => activate(item.id)}>激活此 revision</Button> : <span className="muted-copy">immutable</span>}</td></tr>)}</tbody></table></div>}</section>;
}
