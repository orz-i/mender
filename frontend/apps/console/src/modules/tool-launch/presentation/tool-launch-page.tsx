import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { LaunchLoginRequiredError, type LaunchGateway, type LaunchRiskReview, type PreparedRunStart, type StartedRun } from '../application/launch-gateway';
import { launchOptionKey } from '../domain/launch';

function message(error: unknown) { return error instanceof Error ? error.message : 'Tool 启动请求失败。'; }

function parseArguments(raw: string) {
  const value: unknown = JSON.parse(raw);
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Arguments 必须是 JSON object。');
  return value as Record<string, unknown>;
}

const riskLabel = { low: '低', medium: '中', high: '高', critical: '关键' } as const;
const outcomeLabel = { allow: '允许执行', confirmation_required: '需要人工确认', deny: '拒绝执行' } as const;
const reasonLabel: Record<string, string> = {
  tool_read_only_safe: '只读且安全重试', tool_read_only_non_safe: '只读但幂等合同较弱', tool_write_idempotent: '写操作且可幂等重试', tool_write_unsafe: '不可安全重试的写操作',
  tool_write_contract_mismatch: '写操作合同不完整', within_unconfirmed_risk: '风险在无需确认的策略阈值内', human_confirmation_required: '当前策略要求 Human 显式确认',
  machine_confirmation_unavailable: '非 Human 调用不能提供人工确认', unsafe_write_denied: '当前策略禁止 unsafe write',
};

function RiskStatus({ value }: { value: LaunchRiskReview }) {
  const decision = value.decision;
  const className = decision.outcome === 'allow' ? 'success-panel' : 'error-panel';
  return <div className={className} role="status"><strong>服务端执行风险：{riskLabel[decision.riskLevel]} · {outcomeLabel[decision.outcome]}</strong>
    <span>Policy <span className="mono">{decision.policyRevisionId}</span> revision {decision.policyRevision} · decision #{decision.sequence}</span>
    <span>Arguments SHA-256 <span className="mono">{decision.argumentsHash}</span></span>
    {decision.reasonCodes.map((reason) => <span key={reason}>{reasonLabel[reason] ?? reason}</span>)}
    {value.confirmationId && <span>Confirmation <span className="mono">{value.confirmationId}</span> · expires {value.confirmationExpiresAt ?? '—'}</span>}
  </div>;
}

function money(micro: string, currency: string) {
  const value = BigInt(micro);
  const whole = value / 1_000_000n;
  const fraction = (value % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '');
  return `${currency} ${whole}${fraction ? `.${fraction}` : ''}`;
}

export function ToolLaunchPage({ gateway }: { gateway: LaunchGateway }) {
  const [params] = useSearchParams();
  const workspaces = useQuery({ queryKey: ['launch-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const initialWorkspace = params.get('workspace') ?? '';
  const [selection, setSelection] = useState(initialWorkspace);
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const options = useQuery({ queryKey: ['launch-options', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.options(workspaceId, signal), retry: false });
  const [optionKey, setOptionKey] = useState('');
  const selected = useMemo(() => {
    const items = options.data ?? [];
    return items.find((item) => launchOptionKey(item) === optionKey) ?? items[0] ?? null;
  }, [options.data, optionKey]);
  const [argumentsText, setArgumentsText] = useState('{}');
  const [maxCharge, setMaxCharge] = useState('');
  const [prepared, setPrepared] = useState<PreparedRunStart | null>(null);
  const [risk, setRisk] = useState<LaunchRiskReview | null>(null);
  const [result, setResult] = useState<StartedRun | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const effectiveCap = maxCharge || selected?.reserveMicro || '';
  const locked = prepared !== null || busy;

  const resetPrepared = async () => {
    const current = prepared;
    setPrepared(null); setRisk(null); setError(null);
    if (current) {
      try { await gateway.revoke(current); } catch { /* exact key + TTL still prevents a second logical Run */ }
    }
  };

  const startPrepared = async (review: LaunchRiskReview, args: Record<string, unknown>) => {
    if (!selected || !workspaceId || review.decision.outcome === 'deny') return;
    const current = prepared ?? await gateway.prepare(workspaceId, selected, effectiveCap, review.decision.argumentsHash, review.idempotencyKey);
    if (!prepared) setPrepared(current);
    const started = await gateway.submit(current, args);
    setResult(started);
    try { await gateway.revoke(current); } catch { /* exact key + hash + TTL still prevent a second logical Run */ }
    setPrepared(null);
  };

  const previewAndMaybeStart = async () => {
    if (!selected || !workspaceId) return;
    setBusy(true); setError(null); setResult(null);
    try {
      const args = parseArguments(argumentsText);
      if (prepared) {
        if (!risk || risk.idempotencyKey !== prepared.idempotencyKey || risk.decision.argumentsHash !== prepared.argumentsHash) throw new Error('执行风险证据与启动委托不一致，请撤销后重新评估。');
        await startPrepared(risk, args);
        return;
      }
      const review = await gateway.preview(workspaceId, selected, args);
      setRisk(review);
      if (review.decision.outcome === 'allow') await startPrepared(review, args);
    } catch (cause) { setError(cause); }
    finally { setBusy(false); }
  };

  const confirmAndStart = async () => {
    if (!selected || !workspaceId || !risk || risk.decision.outcome !== 'confirmation_required') return;
    setBusy(true); setError(null); setResult(null);
    try {
      const args = parseArguments(argumentsText);
      const confirmed = await gateway.confirm(workspaceId, selected, args, risk.idempotencyKey);
      setRisk(confirmed);
      if (confirmed.decision.argumentsHash !== risk.decision.argumentsHash) throw new Error('服务端确认与当前 Arguments 不一致，请重新评估。');
      if (confirmed.decision.outcome === 'deny') return;
      if (confirmed.decision.outcome === 'confirmation_required' && !confirmed.confirmationId) throw new Error('服务端未返回所需的危险动作确认。');
      await startPrepared(confirmed, args);
    } catch (cause) { setError(cause); }
    finally { setBusy(false); }
  };

  function resetRisk() { setRisk(null); setResult(null); setError(null); }

  if (workspaces.error instanceof LaunchLoginRequiredError || options.error instanceof LaunchLoginRequiredError) return <>
    <p className="eyebrow">Mender / Launch</p><h1>登录 Console</h1><p className="lead">Tool 启动使用显式 Human capability，不接受页面手填 Machine Key。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button>
  </>;

  return <>
    <p className="eyebrow">Mender / Launch</p>
    <div className="page-heading-row"><div><h1>启动工具</h1><p className="lead">从服务端过滤后的 Toolset、ToolVersion 与 Connection 中选择目标。最终 Schema、授权、价格、Budget 与额度仍由 Admission 重新裁决。</p></div></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length > 0 && <section className="launch-panel">
      <div className="launch-toolbar"><label>Workspace<select disabled={locked} value={workspaceId} onChange={(event) => { setSelection(event.target.value); setOptionKey(''); setMaxCharge(''); resetRisk(); }}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label></div>
      <div className="success-panel" role="note"><strong>Execution risk 与 run:create 权限全部由服务端裁决</strong><span>页面只展示服务端 risk/outcome/reasons；Workspace role、side-effect 与 idempotency 不用于浏览器本地授权或风险计算。</span></div>
      <>
        {options.isPending && <div className="empty-state"><strong>正在解析可启动能力…</strong></div>}
        {options.error && <div className="error-panel" role="alert">{message(options.error)}</div>}
        {options.data && options.data.length === 0 && <div className="empty-state"><strong>没有可启动能力</strong><span>需要发布中的 Toolset/ToolVersion、当前用户的 Connection Grant、PriceVersion 与 Budget window。</span></div>}
        {selected && <div className="launch-grid">
          <div className="launch-options"><label>Tool / Connection<select disabled={locked} value={launchOptionKey(selected)} onChange={(event) => { setOptionKey(event.target.value); setMaxCharge(''); resetRisk(); }}>{options.data!.map((item) => <option key={launchOptionKey(item)} value={launchOptionKey(item)}>{item.title} · {item.connectionId}</option>)}</select></label>
            <article className="launch-card"><p className="section-kicker">{selected.sideEffect === 'read_only' ? 'Read only' : 'Write'} · {selected.idempotency}</p><h2>{selected.title}</h2><p>{selected.description || '无描述'}</p><dl className="fact-grid"><div><dt>Tool</dt><dd className="mono">{selected.toolId}@{selected.toolVersion}</dd></div><div><dt>Toolset</dt><dd className="mono">{selected.toolsetVersionId}</dd></div><div><dt>Provider</dt><dd className="mono">{selected.providerId}</dd></div><div><dt>Connection</dt><dd className="mono">{selected.connectionId}</dd></div></dl></article>
          </div>
          <div className="launch-request"><label>Arguments JSON<textarea disabled={locked} value={argumentsText} onChange={(event) => { setArgumentsText(event.target.value); resetRisk(); }} spellCheck={false} rows={12} /></label><details><summary>输入 Schema</summary><pre className="schema-preview">{JSON.stringify(selected.inputSchema, null, 2)}</pre></details>
            <label>本次最大费用（micro units）<input disabled={locked} inputMode="numeric" value={effectiveCap} onChange={(event) => setMaxCharge(event.target.value)} /></label><p className="muted-copy">当前 reserve quote：{money(selected.reserveMicro, selected.currency)}。提高上限不会改变服务端价格；低于实际 reserve 会被 Admission 拒绝。</p>
            <div className="credential-actions">{risk?.decision.outcome === 'confirmation_required' && !prepared ? <Button type="button" disabled={busy || !effectiveCap} onClick={() => void confirmAndStart()}>{busy ? '正在确认…' : '确认危险动作并启动'}</Button> : risk?.decision.outcome === 'deny' && !prepared ? <Button type="button" disabled>策略拒绝</Button> : <Button type="button" disabled={busy || !effectiveCap} onClick={() => void previewAndMaybeStart()}>{busy ? '正在评估…' : prepared ? '使用同一幂等请求重试' : '评估风险并启动'}</Button>}{prepared && <Button type="button" variant="outline" disabled={busy} onClick={() => void resetPrepared()}>撤销并重置</Button>}</div>
            {risk && <RiskStatus value={risk} />}
            {prepared && <p className="muted-copy">前一次响应未确认。短时 token 只保存在本页内存；重试继续使用同一 Idempotency-Key，不会创建第二个逻辑 Run。</p>}
            {error !== null && <div className="error-panel" role="alert">{message(error)}</div>}
            {result && <div className="success-panel" role="status"><strong>Run 已受理：<span className="mono">{result.runId}</span></strong><span>{result.replayed ? '已通过相同幂等请求收敛。' : '预算已预留，等待 Worker 执行。'}</span><Button asChild variant="outline"><Link to={`/runs?workspace=${encodeURIComponent(workspaceId)}`}>打开 Run Explorer</Link></Button></div>}
          </div>
        </div>}
      </>
    </section>}
  </>;
}
