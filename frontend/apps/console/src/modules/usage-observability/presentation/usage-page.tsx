import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { UsageLoginRequiredError, type UsageGateway } from '../application/usage-gateway';
import type { BudgetPeriod, QuotaState, UsageEntry } from '../domain/usage';

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Usage / Budget 数据暂时不可用。';
}

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date);
}

function formatMicro(value: string, currency: string) {
  const micro = BigInt(value);
  const whole = micro / 1_000_000n;
  const fraction = (micro % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '');
  return `${currency} ${whole}${fraction ? `.${fraction}` : ''}`;
}

const quotaLabels: Record<QuotaState, string> = { held: '已预留', released: '已释放', settled: '已结算' };

function BudgetCard({ period }: { period: BudgetPeriod }) {
  return <article className="budget-card">
    <div className="budget-card-heading"><div><p className="section-kicker">{period.active ? 'Active period' : 'Historical period'}</p><h3 className="mono">{period.budgetId}</h3></div><span>v{period.revision}</span></div>
    <div className="budget-amount"><span>可用额度</span><strong>{formatMicro(period.availableMicro, period.currency)}</strong></div>
    <dl className="usage-fact-grid">
      <div><dt>Limit</dt><dd>{formatMicro(period.limitMicro, period.currency)}</dd></div>
      <div><dt>Consumed</dt><dd>{formatMicro(period.consumedMicro, period.currency)}</dd></div>
      <div><dt>Reserved</dt><dd>{formatMicro(period.reservedMicro, period.currency)}</dd></div>
      <div><dt>Period</dt><dd>{formatTime(period.startsAt)} — {formatTime(period.endsAt)}</dd></div>
    </dl>
  </article>;
}

function UsageTable({ items, workspaceId }: { items: UsageEntry[]; workspaceId: string }) {
  if (items.length === 0) return <div className="empty-state"><strong>还没有 quota 使用记录</strong><span>Run 被 Admission 受理后会产生 reservation 事实。</span></div>;
  return <div className="usage-table-wrap"><table className="usage-table">
    <thead><tr><th>Run</th><th>Quota 状态</th><th>Reserved</th><th>Charged</th><th>Released</th><th>时间</th></tr></thead>
    <tbody>{items.map((item) => <tr key={item.runId}>
      <td><span className="mono">{item.runId}</span><small>{item.outcome ?? '—'}</small></td>
      <td><span className={`quota-badge quota-${item.quotaState}`}>{quotaLabels[item.quotaState]}</span></td>
      <td>{formatMicro(item.reservedMicro, item.currency)}</td>
      <td>{item.chargedMicro === null ? '—' : formatMicro(item.chargedMicro, item.currency)}</td>
      <td>{formatMicro(item.releasedMicro, item.currency)}</td>
      <td><span>{formatTime(item.finalizedAt ?? item.createdAt)}</span><Button asChild variant="outline"><Link to={`/runs?workspace=${encodeURIComponent(workspaceId)}`}>Run</Link></Button></td>
    </tr>)}</tbody>
  </table></div>;
}

export function UsagePage({ gateway }: { gateway: UsageGateway }) {
  const [params] = useSearchParams();
  const initialWorkspace = params.get('workspace') ?? '';
  const [selection, setSelection] = useState(initialWorkspace);
  const workspaces = useQuery({ queryKey: ['usage-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const snapshot = useQuery({
    queryKey: ['usage-snapshot', workspaceId], enabled: workspaceId !== '', retry: false,
    queryFn: ({ signal }) => gateway.snapshot(workspaceId, signal),
  });

  if (workspaces.error instanceof UsageLoginRequiredError || snapshot.error instanceof UsageLoginRequiredError) return <>
    <p className="eyebrow">Mender / Usage</p><h1>登录 Console</h1>
    <p className="lead">额度可视化使用当前 Human Browser Session；不会要求 Machine Key 或暴露支付凭据。</p>
    <Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button>
  </>;

  return <>
    <p className="eyebrow">Mender / Usage</p>
    <div className="page-heading-row"><div><h1>额度与用量</h1><p className="lead">查看 Mender quota 的 Budget window、预留、实际消费与释放。这里不是支付钱包、收入账本或发票系统。</p></div><Button type="button" variant="outline" disabled={!workspaceId || snapshot.isFetching} onClick={() => void snapshot.refetch()}>刷新</Button></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && !(workspaces.error instanceof UsageLoginRequiredError) && <div className="error-panel" role="alert">{errorMessage(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length === 0 && <div className="empty-state"><strong>没有可访问 Workspace</strong><span>请联系 Workspace Owner 或平台管理员。</span></div>}
    {workspaces.data && workspaces.data.length > 0 && <>
      <section className="usage-toolbar" aria-label="Usage workspace">
        <label>Workspace<select value={workspaceId} onChange={(event) => setSelection(event.target.value)}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label>
        <p>服务端每次重新校验 usage:read membership，并通过独立 RLS 数据库角色读取 Commerce。</p>
      </section>
      {snapshot.isPending && <div className="empty-state"><strong>正在读取 quota 快照…</strong></div>}
      {snapshot.error && !(snapshot.error instanceof UsageLoginRequiredError) && <div className="error-panel" role="alert">{errorMessage(snapshot.error)}</div>}
      {snapshot.data && <>
        <section aria-labelledby="budget-periods-heading"><div className="panel-heading"><div><p className="section-kicker">Quota windows</p><h2 id="budget-periods-heading">Budget periods</h2></div><span>{snapshot.data.budgetPeriods.length} 个</span></div>
          {snapshot.data.budgetPeriods.length === 0 ? <div className="empty-state"><strong>没有 Budget period</strong><span>本页没有创建或调额权限。</span></div> : <div className="budget-card-grid">{snapshot.data.budgetPeriods.map((period) => <BudgetCard key={`${period.budgetId}:${period.periodId}`} period={period} />)}</div>}
        </section>
        <section className="usage-history" aria-labelledby="usage-history-heading"><div className="panel-heading"><div><p className="section-kicker">Recent reservations</p><h2 id="usage-history-heading">最近用量</h2></div><span>最多 100 条</span></div><UsageTable items={snapshot.data.usageEntries} workspaceId={workspaceId} /></section>
      </>}
    </>}
  </>;
}
