import { useState } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { HistoryLoginRequiredError, type HistoryGateway } from '../application/history-gateway';
import type { PublicationAuditEvent, PublicationAuditEventKind, PublicationAuditTargetKind, PublicationHistoryFilter } from '../domain/history';

const eventLabel: Record<PublicationAuditEventKind, string> = {
  audit_baseline: '审计基线', approval_submitted: '提交审核', approval_approved: '批准', approval_rejected: '拒绝',
  approval_expired: '审批失效', approval_consumed: '消费审批', publication_committed: '发布完成', publication_retired: '退役',
};
const reasonLabel: Record<string, string> = { revision_drift: 'Revision 已变化', target_deleted: '目标已删除', ttl_elapsed: '审批已过期' };
function message(error: unknown) { return error instanceof Error ? error.message : 'Publication history 加载失败。'; }
function formatTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(date); }

type FilterForm = { targetKind: '' | PublicationAuditTargetKind; targetId: string; approvalId: string; eventKind: '' | PublicationAuditEventKind };
function initialFilter(params: URLSearchParams): FilterForm {
  const kind = params.get('target_kind');
  const event = params.get('event_kind');
  return {
    targetKind: kind === 'tool_version' || kind === 'toolset' ? kind : '',
    targetId: params.get('target_id') ?? '', approvalId: params.get('approval') ?? '',
    eventKind: ['audit_baseline', 'approval_submitted', 'approval_approved', 'approval_rejected', 'approval_expired', 'approval_consumed', 'publication_committed', 'publication_retired'].includes(event ?? '') ? event as PublicationAuditEventKind : '',
  };
}
function requestFilter(form: FilterForm, beforeSequence?: string): PublicationHistoryFilter {
  return { targetKind: form.targetKind || undefined, targetId: form.targetId || undefined, approvalId: form.approvalId || undefined, eventKind: form.eventKind || undefined, beforeSequence, limit: 50 };
}

export function PublicationHistoryPage({ gateway }: { gateway: HistoryGateway }) {
  const [params] = useSearchParams();
  const initial = initialFilter(params);
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const [draft, setDraft] = useState<FilterForm>(initial);
  const [applied, setApplied] = useState<FilterForm>(initial);
  const workspaces = useQuery({ queryKey: ['admin-history-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const history = useInfiniteQuery({
    queryKey: ['admin-publication-history', workspaceId, applied], enabled: workspaceId !== '', initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) => gateway.list(workspaceId, requestFilter(applied, typeof pageParam === 'string' ? pageParam : undefined), signal),
    getNextPageParam: (lastPage) => lastPage.nextBeforeSequence ?? undefined, retry: false,
  });
  const loginRequired = workspaces.error instanceof HistoryLoginRequiredError || history.error instanceof HistoryLoginRequiredError;
  const events = history.data?.pages.flatMap((page) => page.events) ?? [];

  if (loginRequired) return <><p className="eyebrow">Mender / Admin / Governance</p><h1>登录 Admin</h1><p className="lead">Publication history 使用受保护 Human OIDC session；审计读取权限由服务端 Workspace membership 重新授权。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button></>;

  return <>
    <p className="eyebrow">Mender / Admin / Governance</p>
    <div className="page-heading-row"><div><h1>发布审计历史</h1><p className="lead">查看服务端持久化的 append-only publication audit timeline。当前 Approval 状态与历史事件分开建模；页面不会根据 Approval 行自行重建历史。</p></div><Button asChild variant="outline"><Link to="/publication-reviews">返回发布审核</Link></Button></div>
    <div className="success-panel" role="note"><strong>Append-only Governance Facts</strong><span>Submitted、Decision、Expiry / Revision drift、Consume、Commit 与 Retire 由后端事务写入；Sequence 与 Revision 始终以精确十进制字符串展示。</span></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && !(workspaces.error instanceof HistoryLoginRequiredError) && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length === 0 && <div className="empty-state"><strong>没有可访问 Workspace</strong></div>}
    {workspaces.data && workspaces.data.length > 0 && <>
      <section className="run-list-panel" aria-label="Publication history filters">
        <div className="panel-heading"><div><p className="section-kicker">History Query</p><h2>Workspace 与服务端过滤</h2></div><div className="run-controls"><label>Workspace<select value={workspaceId} onChange={(event) => setSelection(event.target.value)}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label></div></div>
        <div className="run-controls">
          <label>Target kind<select value={draft.targetKind} onChange={(event) => setDraft({ ...draft, targetKind: event.target.value as FilterForm['targetKind'] })}><option value="">全部</option><option value="tool_version">ToolVersion</option><option value="toolset">Toolset</option></select></label>
          <label>Target ID<input value={draft.targetId} onChange={(event) => setDraft({ ...draft, targetId: event.target.value })} placeholder="例如 tv_search_v1" /></label>
          <label>Approval ID<input value={draft.approvalId} onChange={(event) => setDraft({ ...draft, approvalId: event.target.value })} placeholder="例如 approval_..." /></label>
          <label>Event<select value={draft.eventKind} onChange={(event) => setDraft({ ...draft, eventKind: event.target.value as FilterForm['eventKind'] })}><option value="">全部</option>{Object.entries(eventLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
          <Button type="button" onClick={() => setApplied({ ...draft })}>应用过滤</Button>
          <Button type="button" variant="outline" onClick={() => { const empty: FilterForm = { targetKind: '', targetId: '', approvalId: '', eventKind: '' }; setDraft(empty); setApplied(empty); }}>清除</Button>
        </div>
      </section>
      {history.isPending && <div className="empty-state"><strong>正在读取 append-only publication history…</strong></div>}
      {history.error && !(history.error instanceof HistoryLoginRequiredError) && <div className="error-panel" role="alert">{message(history.error)}</div>}
      {!history.isPending && !history.error && events.length === 0 && <div className="empty-state"><strong>当前过滤条件下没有审计事件</strong></div>}
      {events.length > 0 && <section className="run-list-panel" aria-label="Publication audit timeline"><div className="panel-heading"><div><p className="section-kicker">Audit Timeline</p><h2>不可变治理时间线</h2></div><span className="muted-copy">已加载 {events.length} 条</span></div><HistoryTable events={events} />{history.hasNextPage && <div className="credential-actions"><Button type="button" variant="outline" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{history.isFetchingNextPage ? '正在加载…' : '加载更早记录'}</Button></div>}</section>}
    </>}
  </>;
}

function HistoryTable({ events }: { events: PublicationAuditEvent[] }) {
  return <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Sequence / Event</th><th>Target</th><th>Revision</th><th>Actor / Reason</th><th>时间 / Note</th></tr></thead><tbody>{events.map((event) => <tr key={event.sequence}>
    <td><strong>{eventLabel[event.eventKind]}</strong><div className="mono">#{event.sequence}</div><div className="muted-copy">{event.approvalId ?? '无 approval'}</div></td>
    <td><strong>{event.targetKind === 'tool_version' ? 'ToolVersion' : 'Toolset'}</strong><div className="mono">{event.targetId}</div></td>
    <td><span className="mono">{event.targetRevision}</span>{event.observedRevision && event.observedRevision !== event.targetRevision && <div className="muted-copy">observed → <span className="mono">{event.observedRevision}</span></div>}</td>
    <td><div className="mono">{event.actorUserId ?? 'system'}</div><div className="muted-copy">{reasonLabel[event.reasonCode] ?? (event.reasonCode || '—')}</div></td>
    <td><div>{formatTime(event.occurredAt)}</div><div className="muted-copy">{event.note || '无 note'}</div></td>
  </tr>)}</tbody></table></div>;
}

