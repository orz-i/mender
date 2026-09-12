import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { LaunchLoginRequiredError, type LaunchGateway, type PreparedRunStart, type StartedRun } from '../application/launch-gateway';
import { canStartRuns, launchOptionKey } from '../domain/launch';

function message(error: unknown) { return error instanceof Error ? error.message : 'Tool 启动请求失败。'; }

function parseArguments(raw: string) {
  const value: unknown = JSON.parse(raw);
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Arguments 必须是 JSON object。');
  return value as Record<string, unknown>;
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
  const membership = workspaces.data?.find((item) => item.id === workspaceId);
  const options = useQuery({ queryKey: ['launch-options', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.options(workspaceId, signal), retry: false });
  const [optionKey, setOptionKey] = useState('');
  const selected = useMemo(() => {
    const items = options.data ?? [];
    return items.find((item) => launchOptionKey(item) === optionKey) ?? items[0] ?? null;
  }, [options.data, optionKey]);
  const [argumentsText, setArgumentsText] = useState('{}');
  const [maxCharge, setMaxCharge] = useState('');
  const [prepared, setPrepared] = useState<PreparedRunStart | null>(null);
  const [result, setResult] = useState<StartedRun | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const effectiveCap = maxCharge || selected?.reserveMicro || '';
  const locked = prepared !== null || busy;

  const resetPrepared = async () => {
    const current = prepared;
    setPrepared(null); setError(null);
    if (current) {
      try { await gateway.revoke(current); } catch { /* exact key + TTL still prevents a second logical Run */ }
    }
  };

  const submit = async () => {
    if (!selected || !workspaceId) return;
    setBusy(true); setError(null); setResult(null);
    try {
      const args = parseArguments(argumentsText);
      const current = prepared ?? await gateway.prepare(workspaceId, selected, effectiveCap);
      if (!prepared) setPrepared(current);
      const started = await gateway.submit(current, args);
      setResult(started);
      try { await gateway.revoke(current); } catch { /* capability is idempotency-bound and expires shortly */ }
      setPrepared(null);
    } catch (cause) { setError(cause); }
    finally { setBusy(false); }
  };

  if (workspaces.error instanceof LaunchLoginRequiredError || options.error instanceof LaunchLoginRequiredError) return <>
    <p className="eyebrow">Mender / Launch</p><h1>登录 Console</h1><p className="lead">Tool 启动使用显式 Human capability，不接受页面手填 Machine Key。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button>
  </>;

  return <>
    <p className="eyebrow">Mender / Launch</p>
    <div className="page-heading-row"><div><h1>启动工具</h1><p className="lead">从服务端过滤后的 Toolset、ToolVersion 与 Connection 中选择目标。最终 Schema、授权、价格、Budget 与额度仍由 Admission 重新裁决。</p></div></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length > 0 && <section className="launch-panel">
      <div className="launch-toolbar"><label>Workspace<select disabled={locked} value={workspaceId} onChange={(event) => { setSelection(event.target.value); setOptionKey(''); setMaxCharge(''); setResult(null); }}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label></div>
      {!membership || !canStartRuns(membership.role) ? <div className="empty-state"><strong>当前 Membership 仅允许查看</strong><span>Human run:create 只授予 Owner、Admin 与 Developer。</span></div> : <>
        {options.isPending && <div className="empty-state"><strong>正在解析可启动能力…</strong></div>}
        {options.error && <div className="error-panel" role="alert">{message(options.error)}</div>}
        {options.data && options.data.length === 0 && <div className="empty-state"><strong>没有可启动能力</strong><span>需要发布中的 Toolset/ToolVersion、当前用户的 Connection Grant、PriceVersion 与 Budget window。</span></div>}
        {selected && <div className="launch-grid">
          <div className="launch-options"><label>Tool / Connection<select disabled={locked} value={launchOptionKey(selected)} onChange={(event) => { setOptionKey(event.target.value); setMaxCharge(''); setResult(null); }}>{options.data!.map((item) => <option key={launchOptionKey(item)} value={launchOptionKey(item)}>{item.title} · {item.connectionId}</option>)}</select></label>
            <article className="launch-card"><p className="section-kicker">{selected.sideEffect === 'read_only' ? 'Read only' : 'Write'} · {selected.idempotency}</p><h2>{selected.title}</h2><p>{selected.description || '无描述'}</p><dl className="fact-grid"><div><dt>Tool</dt><dd className="mono">{selected.toolId}@{selected.toolVersion}</dd></div><div><dt>Toolset</dt><dd className="mono">{selected.toolsetVersionId}</dd></div><div><dt>Provider</dt><dd className="mono">{selected.providerId}</dd></div><div><dt>Connection</dt><dd className="mono">{selected.connectionId}</dd></div></dl></article>
          </div>
          <div className="launch-request"><label>Arguments JSON<textarea disabled={locked} value={argumentsText} onChange={(event) => setArgumentsText(event.target.value)} spellCheck={false} rows={12} /></label><details><summary>输入 Schema</summary><pre className="schema-preview">{JSON.stringify(selected.inputSchema, null, 2)}</pre></details>
            <label>本次最大费用（micro units）<input disabled={locked} inputMode="numeric" value={effectiveCap} onChange={(event) => setMaxCharge(event.target.value)} /></label><p className="muted-copy">当前 reserve quote：{money(selected.reserveMicro, selected.currency)}。提高上限不会改变服务端价格；低于实际 reserve 会被 Admission 拒绝。</p>
            <div className="credential-actions"><Button type="button" disabled={busy || !effectiveCap} onClick={() => void submit()}>{busy ? '正在受理…' : prepared ? '使用同一幂等请求重试' : '授权并启动'}</Button>{prepared && <Button type="button" variant="outline" disabled={busy} onClick={() => void resetPrepared()}>撤销并重置</Button>}</div>
            {prepared && <p className="muted-copy">前一次响应未确认。短时 token 只保存在本页内存；重试继续使用同一 Idempotency-Key，不会创建第二个逻辑 Run。</p>}
            {error !== null && <div className="error-panel" role="alert">{message(error)}</div>}
            {result && <div className="success-panel" role="status"><strong>Run 已受理：<span className="mono">{result.runId}</span></strong><span>{result.replayed ? '已通过相同幂等请求收敛。' : '预算已预留，等待 Worker 执行。'}</span><Button asChild variant="outline"><Link to={`/runs?workspace=${encodeURIComponent(workspaceId)}`}>打开 Run Explorer</Link></Button></div>}
          </div>
        </div>}
      </>}
    </section>}
  </>;
}
