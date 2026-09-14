import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { ExecutionGovernanceLoginRequiredError, type ExecutionGovernanceGateway } from '../application/execution-governance-gateway';
import type { ExecutionConfirmation, ExecutionConfirmationState, ExecutionGovernanceFilter, ExecutionOutcome, ExecutionPolicyInput, ExecutionPolicyRevision, ExecutionRiskLevel, ExecutionSubjectKind } from '../domain/execution-governance';

const risks: ExecutionRiskLevel[] = ['low', 'medium', 'high', 'critical'];
const states = { draft: '草稿', active: '生效中', retired: '已退役' } as const;
function message(error: unknown) { return error instanceof Error ? error.message : 'Execution governance 操作失败。'; }
function formatTime(value: string | null) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }

interface FilterDraft {
  toolVersionId: string; subjectKind: '' | ExecutionSubjectKind; riskLevel: '' | ExecutionRiskLevel; outcome: '' | ExecutionOutcome;
  policyRevision: string; confirmationState: '' | ExecutionConfirmationState;
}
const emptyFilters: FilterDraft = { toolVersionId: '', subjectKind: '', riskLevel: '', outcome: '', policyRevision: '', confirmationState: '' };
function appliedFilters(draft: FilterDraft): ExecutionGovernanceFilter {
  return {
    ...(draft.toolVersionId ? { toolVersionId: draft.toolVersionId } : {}),
    ...(draft.subjectKind ? { subjectKind: draft.subjectKind } : {}),
    ...(draft.riskLevel ? { riskLevel: draft.riskLevel } : {}),
    ...(draft.outcome ? { outcome: draft.outcome } : {}),
    ...(draft.policyRevision ? { policyRevision: draft.policyRevision } : {}),
    ...(draft.confirmationState ? { confirmationState: draft.confirmationState } : {}),
    limit: 50,
  };
}

export function ExecutionGovernancePage({ gateway }: { gateway: ExecutionGovernanceGateway }) {
  const queryClient = useQueryClient(); const [params] = useSearchParams();
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const [filterDraft, setFilterDraft] = useState<FilterDraft>(emptyFilters);
  const [filters, setFilters] = useState<ExecutionGovernanceFilter>({ limit: 50 });
  const [form, setForm] = useState<ExecutionPolicyInput>({ id: '', maxUnconfirmedRiskLevel: 'high', maxMachineRiskLevel: 'low', denyUnsafeWrite: true, confirmationTtlSeconds: 120 });
  const workspaces = useQuery({ queryKey: ['admin-execution-governance-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const snapshot = useQuery({ queryKey: ['admin-execution-governance', workspaceId, filters], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.snapshot(workspaceId, filters, signal), retry: false });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin-execution-governance', workspaceId] });
  const create = useMutation({ mutationFn: () => gateway.create(workspaceId, form), onSuccess: async () => { setForm((current) => ({ ...current, id: '' })); await refresh(); } });
  const activate = useMutation({ mutationFn: (id: string) => gateway.activate(workspaceId, id), onSuccess: refresh });
  const loginRequired = workspaces.error instanceof ExecutionGovernanceLoginRequiredError || snapshot.error instanceof ExecutionGovernanceLoginRequiredError;

  if (loginRequired) return <><p className="eyebrow">Mender / Admin / Execution Governance</p><h1>登录 Admin</h1><p className="lead">Execution governance 使用受保护 Human OIDC session；管理权限由服务端 Workspace membership 与受限 Governance role 重新授权。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button></>;

  return <>
    <p className="eyebrow">Mender / Admin / Execution Governance</p>
    <div className="page-heading-row"><div><h1>执行治理 / 风险策略</h1><p className="lead">管理不可变的 ExecutionPolicyRevision，并观察服务端对实际执行产生的风险决议与 Human confirmation 生命周期。页面不根据 ToolVersion、用户角色或参数内容自行推断风险、权限或最终 outcome。</p></div></div>
    <div className="success-panel" role="note"><strong>Server-owned execution decision</strong><span>风险等级、allow / confirmation_required / deny、reason codes 与 confirmation state 均来自服务端；策略只包含声明式风险上限、unsafe-write 开关与有界 TTL，不提供脚本、SQL、参数内容规则或支付审批。</span></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && !(workspaces.error instanceof ExecutionGovernanceLoginRequiredError) && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length === 0 && <div className="empty-state"><strong>当前账号没有可用 Workspace</strong></div>}
    {workspaces.data && workspaces.data.length > 0 && <>
      <section className="run-list-panel" aria-label="Execution governance workspace"><div className="panel-heading"><div><p className="section-kicker">Workspace</p><h2>Execution governance boundary</h2></div><div className="run-controls"><label>Workspace<select value={workspaceId} onChange={(event) => setSelection(event.target.value)}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label><Button type="button" variant="outline" disabled={snapshot.isFetching} onClick={() => void snapshot.refetch()}>刷新</Button></div></div></section>
      {snapshot.error && !(snapshot.error instanceof ExecutionGovernanceLoginRequiredError) && <div className="error-panel" role="alert">{message(snapshot.error)}</div>}
      {create.error && <div className="error-panel" role="alert">{message(create.error)}</div>}{activate.error && <div className="error-panel" role="alert">{message(activate.error)}</div>}
      {snapshot.data?.activePolicy && <ActivePolicy item={snapshot.data.activePolicy} />}
      <section className="launch-panel" aria-labelledby="execution-policy-create-heading"><div className="panel-heading"><div><p className="section-kicker">Policy operations</p><h2 id="execution-policy-create-heading">创建 ExecutionPolicyRevision 草稿</h2></div></div><div className="launch-grid"><div className="launch-request"><label>Policy ID<input value={form.id} onChange={(event) => setForm({ ...form, id: event.target.value })} /></label><label>Human 无确认风险上限<select value={form.maxUnconfirmedRiskLevel} onChange={(event) => setForm({ ...form, maxUnconfirmedRiskLevel: event.target.value as ExecutionRiskLevel })}>{risks.map((risk) => <option key={risk} value={risk}>{risk}</option>)}</select></label><label>Machine 风险上限<select value={form.maxMachineRiskLevel} onChange={(event) => setForm({ ...form, maxMachineRiskLevel: event.target.value as ExecutionRiskLevel })}>{risks.map((risk) => <option key={risk} value={risk}>{risk}</option>)}</select></label></div><div className="launch-request"><label>Confirmation TTL（秒，30–600）<input type="number" min={30} max={600} step={1} value={form.confirmationTtlSeconds} onChange={(event) => setForm({ ...form, confirmationTtlSeconds: Number(event.target.value) })} /></label><label><input type="checkbox" checked={form.denyUnsafeWrite} onChange={(event) => setForm({ ...form, denyUnsafeWrite: event.target.checked })} /> deny unsafe write</label><p className="muted-copy">Machine ceiling 与 Human ceiling 的最终合法性由服务端 / 数据库校验；浏览器不会据此计算执行 outcome。</p><Button type="button" disabled={create.isPending || form.id === '' || !Number.isInteger(form.confirmationTtlSeconds) || form.confirmationTtlSeconds < 30 || form.confirmationTtlSeconds > 600} onClick={() => create.mutate()}>创建 policy draft</Button></div></div></section>
      <section className="run-list-panel" aria-label="Execution governance filters"><div className="panel-heading"><div><p className="section-kicker">Server filters</p><h2>执行治理观测筛选</h2></div></div><div className="run-controls"><label>ToolVersion<input value={filterDraft.toolVersionId} onChange={(event) => setFilterDraft({ ...filterDraft, toolVersionId: event.target.value })} /></label><label>Subject<select value={filterDraft.subjectKind} onChange={(event) => setFilterDraft({ ...filterDraft, subjectKind: event.target.value as FilterDraft['subjectKind'] })}><option value="">全部</option><option value="human">human</option><option value="machine">machine</option></select></label><label>Risk<select value={filterDraft.riskLevel} onChange={(event) => setFilterDraft({ ...filterDraft, riskLevel: event.target.value as FilterDraft['riskLevel'] })}><option value="">全部</option>{risks.map((risk) => <option key={risk} value={risk}>{risk}</option>)}</select></label><label>Outcome<select value={filterDraft.outcome} onChange={(event) => setFilterDraft({ ...filterDraft, outcome: event.target.value as FilterDraft['outcome'] })}><option value="">全部</option><option value="allow">allow</option><option value="confirmation_required">confirmation_required</option><option value="deny">deny</option></select></label><label>Policy revision<input inputMode="numeric" value={filterDraft.policyRevision} onChange={(event) => setFilterDraft({ ...filterDraft, policyRevision: event.target.value })} /></label><label>Confirmation state<select value={filterDraft.confirmationState} onChange={(event) => setFilterDraft({ ...filterDraft, confirmationState: event.target.value as FilterDraft['confirmationState'] })}><option value="">全部</option><option value="active">active</option><option value="consumed">consumed</option><option value="expired">expired</option></select></label><Button type="button" onClick={() => setFilters(appliedFilters(filterDraft))}>应用筛选</Button><Button type="button" variant="outline" onClick={() => { setFilterDraft(emptyFilters); setFilters({ limit: 50 }); }}>清除</Button></div></section>
      {snapshot.data && <><RevisionTable items={snapshot.data.revisions} busy={activate.isPending} activate={(id) => activate.mutate(id)} /><DecisionTable items={snapshot.data.decisions} more={snapshot.data.nextBeforeDecisionSequence !== null} /><ConfirmationTable items={snapshot.data.confirmations} more={snapshot.data.nextConfirmationCursor !== null} /></>}
    </>}
  </>;
}

function ActivePolicy({ item }: { item: ExecutionPolicyRevision }) {
  return <section className="welcome-panel" aria-label="Active execution policy"><div><p className="section-kicker">Active policy</p><h2>{item.id} · revision {item.revision}</h2><p>Human unconfirmed ≤ <strong>{item.maxUnconfirmedRiskLevel}</strong> · Machine ≤ <strong>{item.maxMachineRiskLevel}</strong> · confirmation TTL <strong>{item.confirmationTtlSeconds}s</strong> · deny unsafe write <strong>{String(item.denyUnsafeWrite)}</strong></p></div><span className="state-badge state-published">active</span></section>;
}

function RevisionTable({ items, busy, activate }: { items: ExecutionPolicyRevision[]; busy: boolean; activate: (id: string) => void }) {
  return <section className="run-list-panel" aria-label="Execution policy revisions"><div className="panel-heading"><div><p className="section-kicker">Policy history</p><h2>不可变执行策略版本</h2></div></div>{items.length === 0 ? <div className="empty-state"><strong>当前 Workspace 尚无 ExecutionPolicyRevision</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Policy</th><th>声明式规则</th><th>状态</th><th>Lifecycle</th><th>操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong className="mono">{item.id}</strong><div className="muted-copy">revision {item.revision}</div></td><td><div>Human ≤ {item.maxUnconfirmedRiskLevel} · Machine ≤ {item.maxMachineRiskLevel}</div><div className="muted-copy">deny unsafe {String(item.denyUnsafeWrite)} · TTL {item.confirmationTtlSeconds}s</div></td><td><span className={`state-badge state-${item.state === 'active' ? 'published' : item.state === 'retired' ? 'retired' : 'draft'}`}>{states[item.state]}</span></td><td><div>Created {formatTime(item.createdAt)}</div><div className="muted-copy">Activated {formatTime(item.activatedAt)} · Retired {formatTime(item.retiredAt)}</div></td><td>{item.state === 'draft' ? <Button type="button" disabled={busy} onClick={() => activate(item.id)}>激活此 revision</Button> : <span className="muted-copy">immutable</span>}</td></tr>)}</tbody></table></div>}</section>;
}

function DecisionTable({ items, more }: { items: ExecutionGovernanceSnapshotLike['decisions']; more: boolean }) {
  return <section className="run-list-panel" aria-label="Execution policy decisions"><div className="panel-heading"><div><p className="section-kicker">Decisions</p><h2>最近服务端执行风险决议</h2></div>{more && <span className="muted-copy">当前页之后仍有更早记录</span>}</div>{items.length === 0 ? <div className="empty-state"><strong>当前筛选下没有 ExecutionPolicyDecision</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Subject / Decision</th><th>Policy / Risk</th><th>Exact target</th><th>Arguments identity</th><th>Reasons / Time</th></tr></thead><tbody>{items.map((item) => <tr key={item.sequence}><td><strong>{item.subjectKind}</strong><div className="mono">{item.subjectId}</div><span className={`state-badge state-${item.outcome === 'allow' ? 'published' : item.outcome === 'deny' ? 'retired' : 'draft'}`}>{item.outcome}</span><div className="muted-copy">decision {item.sequence}</div></td><td><span className="mono">{item.policyRevisionId}</span><div>revision {item.policyRevision}</div><div className="muted-copy">risk {item.riskLevel}</div></td><td><div className="mono">Toolset {item.toolsetVersionId}</div><div className="mono">ToolVersion {item.toolVersionId}</div><div className="mono">Connection {item.connectionId}</div></td><td><div className="mono">args {item.argumentsHash}</div><div className="mono">idem {item.idempotencyKeyHash}</div></td><td>{item.reasonCodes.map((reason) => <div className="mono" key={reason}>{reason}</div>)}<div className="muted-copy">{formatTime(item.evaluatedAt)}</div></td></tr>)}</tbody></table></div>}</section>;
}

type ExecutionGovernanceSnapshotLike = { decisions: import('../domain/execution-governance').ExecutionPolicyDecision[] };
function ConfirmationTable({ items, more }: { items: ExecutionConfirmation[]; more: boolean }) {
  return <section className="run-list-panel" aria-label="Execution confirmations"><div className="panel-heading"><div><p className="section-kicker">Human confirmations</p><h2>Confirmation 生命周期</h2></div>{more && <span className="muted-copy">当前页之后仍有更早记录</span>}</div>{items.length === 0 ? <div className="empty-state"><strong>当前筛选下没有 Human confirmation</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Confirmation</th><th>Policy / Risk</th><th>Exact target</th><th>Arguments identity</th><th>Lifecycle</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong className="mono">{item.id}</strong><div className="mono">user {item.userId}</div><span className={`state-badge state-${item.state === 'active' ? 'published' : item.state === 'consumed' ? 'draft' : 'retired'}`}>{item.state}</span><div className="muted-copy">persisted {item.persistedState}</div></td><td><span className="mono">{item.policyRevisionId}</span><div>revision {item.policyRevision}</div><div className="muted-copy">risk {item.riskLevel}</div></td><td><div className="mono">Toolset {item.toolsetVersionId}</div><div className="mono">ToolVersion {item.toolVersionId}</div><div className="mono">Connection {item.connectionId}</div></td><td><div className="mono">args {item.argumentsHash}</div><div className="mono">idem {item.idempotencyKeyHash}</div></td><td><div>Created {formatTime(item.createdAt)}</div><div>Expires {formatTime(item.expiresAt)}</div><div className="muted-copy">Consumed {formatTime(item.consumedAt)} · Persisted expired {formatTime(item.expiredAt)}</div></td></tr>)}</tbody></table></div>}</section>;
}
