import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { ReviewLoginRequiredError, type ReviewGateway } from '../application/review-gateway';
import type { PublicationReview, PublicationReviewState } from '../domain/review';

const stateLabel: Record<PublicationReviewState, string> = { pending: '待审核', approved: '已批准', rejected: '已拒绝', consumed: '已发布/消费', expired: '已过期/失效' };
function message(error: unknown) { return error instanceof Error ? error.message : 'Publication review 操作失败。'; }
function formatTime(value: string | null) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }

export function PublicationReviewPage({ gateway }: { gateway: ReviewGateway }) {
  const queryClient = useQueryClient();
  const [params] = useSearchParams();
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const [note, setNote] = useState('');
  const workspaces = useQuery({ queryKey: ['admin-review-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const approvals = useQuery({ queryKey: ['admin-publication-approvals', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.list(workspaceId, signal), retry: false });
  const approve = useMutation({ mutationFn: (id: string) => gateway.approve(workspaceId, id, note), onSuccess: async () => { setNote(''); await queryClient.invalidateQueries({ queryKey: ['admin-publication-approvals', workspaceId] }); } });
  const reject = useMutation({ mutationFn: (id: string) => gateway.reject(workspaceId, id, note), onSuccess: async () => { setNote(''); await queryClient.invalidateQueries({ queryKey: ['admin-publication-approvals', workspaceId] }); } });
  const loginRequired = workspaces.error instanceof ReviewLoginRequiredError || approvals.error instanceof ReviewLoginRequiredError;

  if (loginRequired) return <><p className="eyebrow">Mender / Admin / Governance</p><h1>登录 Admin</h1><p className="lead">Publication review 使用与 Console 相同的受保护 Human OIDC session，但审核权限由服务端 Workspace membership 重新授权。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button></>;

  return <>
    <p className="eyebrow">Mender / Admin / Governance</p>
    <div className="page-heading-row"><div><h1>发布审核</h1><p className="lead">审核 Console 提交的精确 Catalog / Toolset revision。Requester 不能审核自己的请求；Admin 页面不会根据角色自行判定审核权，最终由服务端 catalog:review 裁决。</p></div></div>
    <div className="success-panel" role="note"><strong>Maker / Checker</strong><span>Approve 只批准当前 request 绑定的 target revision；真正 Publish 仍会重新 Preflight，并原子消费 approval。</span></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && !(workspaces.error instanceof ReviewLoginRequiredError) && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length > 0 && <section className="run-list-panel" aria-label="Publication review workspace">
      <div className="panel-heading"><div><p className="section-kicker">Workspace</p><h2>Review boundary</h2></div><div className="run-controls"><label>Workspace<select value={workspaceId} onChange={(event) => setSelection(event.target.value)}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label><Button type="button" variant="outline" disabled={approvals.isFetching} onClick={() => void approvals.refetch()}>刷新</Button></div></div>
      <label>Decision note（最多 1000 字）<textarea rows={3} maxLength={1000} value={note} onChange={(event) => setNote(event.target.value)} /></label>
      {approvals.isPending && <div className="empty-state"><strong>正在读取 publication approvals…</strong></div>}
      {approvals.error && !(approvals.error instanceof ReviewLoginRequiredError) && <div className="error-panel" role="alert">{message(approvals.error)}</div>}
      {approve.error && <div className="error-panel" role="alert">{message(approve.error)}</div>}
      {reject.error && <div className="error-panel" role="alert">{message(reject.error)}</div>}
      {approvals.data && approvals.data.length === 0 && <div className="empty-state"><strong>当前 Workspace 没有发布审核记录</strong></div>}
      {approvals.data && approvals.data.length > 0 && <ReviewTable items={approvals.data} busy={approve.isPending || reject.isPending} approve={(id) => approve.mutate(id)} reject={(id) => reject.mutate(id)} />}
    </section>}
  </>;
}

function ReviewTable({ items, busy, approve, reject }: { items: PublicationReview[]; busy: boolean; approve: (id: string) => void; reject: (id: string) => void }) {
  return <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Target</th><th>Requester</th><th>状态</th><th>时间</th><th>Reviewer / Note</th><th>操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong>{item.targetKind === 'tool_version' ? 'ToolVersion' : 'Toolset'}</strong><div className="mono">{item.targetId}</div><div className="muted-copy">revision {item.targetRevision} · {item.id}</div></td><td className="mono">{item.requesterUserId}</td><td><span className={`state-badge state-${item.state}`}>{stateLabel[item.state]}</span></td><td><div>Requested {formatTime(item.requestedAt)}</div><div className="muted-copy">Expires {formatTime(item.expiresAt)}</div></td><td><div className="mono">{item.reviewerUserId ?? '—'}</div><div className="muted-copy">{item.decisionNote || '无 decision note'}</div></td><td>{item.state === 'pending' ? <div className="credential-actions"><Button type="button" disabled={busy} onClick={() => approve(item.id)}>批准</Button><Button type="button" variant="outline" disabled={busy} onClick={() => reject(item.id)}>拒绝</Button></div> : <span className="muted-copy">只读</span>}</td></tr>)}</tbody></table></div>;
}
