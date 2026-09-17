import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Alert, AlertDescription, AlertTitle, Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle, Field, FieldDescription, FieldGroup, FieldLabel, Input, Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue, Textarea } from '@mender/ui';
import { LaunchLoginRequiredError, type LaunchGateway, type LaunchRiskReview, type PreparedRunStart, type StartedRun } from '../application/launch-gateway';
import { launchOptionKey } from '../domain/launch';

function message(error: unknown) { return error instanceof Error ? error.message : 'Tool 启动请求失败。'; }

function parseArguments(raw: string) {
  const value: unknown = JSON.parse(raw);
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Arguments 必须是 JSON object。');
  return value as Record<string, unknown>;
}

const riskLabel = { low: '低', medium: '中', high: '高', critical: '关键' } as const;
const reasonLabel: Record<string, string> = {
  tool_read_only_safe: '只读且安全重试', tool_read_only_non_safe: '只读但幂等合同较弱', tool_write_idempotent: '写操作且可幂等重试', tool_write_unsafe: '不可安全重试的写操作',
  tool_write_contract_mismatch: '写操作合同不完整', within_unconfirmed_risk: '风险在无需确认的策略阈值内', human_confirmation_required: '当前策略要求 Human 显式确认',
  machine_confirmation_unavailable: '非 Human 调用不能提供人工确认', unsafe_write_denied: '当前策略禁止 unsafe write',
};

interface SimpleInputField {
  name: string;
  label: string;
  description: string;
  required: boolean;
  type: 'string' | 'number' | 'integer';
}

function simpleInputFields(schema: Record<string, unknown>): SimpleInputField[] | null {
  if (schema.type !== 'object' || typeof schema.properties !== 'object' || schema.properties === null || Array.isArray(schema.properties)) return null;
  const required = new Set(Array.isArray(schema.required) ? schema.required.filter((item): item is string => typeof item === 'string') : []);
  const result: SimpleInputField[] = [];
  for (const [name, raw] of Object.entries(schema.properties as Record<string, unknown>)) {
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return null;
    const value = raw as Record<string, unknown>;
    if (value.enum !== undefined || !['string', 'number', 'integer'].includes(String(value.type))) return null;
    result.push({ name, label: typeof value.title === 'string' ? value.title : name.replaceAll('_', ' '), description: typeof value.description === 'string' ? value.description : '', required: required.has(name), type: value.type as SimpleInputField['type'] });
  }
  return result.length > 0 && result.length <= 12 ? result : null;
}

function simpleArguments(fields: SimpleInputField[], values: Record<string, string>) {
  const result: Record<string, string | number> = {};
  for (const field of fields) {
    const raw = values[field.name]?.trim() ?? '';
    if (!raw && !field.required) continue;
    if (field.type === 'string') { result[field.name] = raw; continue; }
    const parsed = Number(raw);
    if (!Number.isFinite(parsed) || (field.type === 'integer' && !Number.isInteger(parsed))) throw new Error(`${field.label} 必须是有效${field.type === 'integer' ? '整数' : '数字'}。`);
    result[field.name] = parsed;
  }
  return result;
}

function RiskStatus({ value }: { value: LaunchRiskReview }) {
  const decision = value.decision;
  return <Alert variant={decision.outcome === 'deny' ? 'destructive' : 'default'}>
    <AlertTitle>{decision.outcome === 'allow' ? 'Ready to run' : decision.outcome === 'confirmation_required' ? 'Confirmation required' : 'Run blocked'} · {riskLabel[decision.riskLevel]} risk</AlertTitle>
    <AlertDescription>{decision.reasonCodes.map((reason) => reasonLabel[reason] ?? reason).join(' · ')}</AlertDescription>
    <details className="risk-details"><summary>Advanced policy details</summary><div>Policy {decision.policyRevisionId} · revision {decision.policyRevision} · decision #{decision.sequence}</div><div className="mono">Arguments {decision.argumentsHash}</div>{value.confirmationId && <div>Confirmation {value.confirmationId} · expires {value.confirmationExpiresAt ?? '—'}</div>}</details>
  </Alert>;
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
  const simpleFields = useMemo(() => selected ? simpleInputFields(selected.inputSchema) : null, [selected]);
  const [simpleValues, setSimpleValues] = useState<Record<string, string>>({});
  const [argumentsText, setArgumentsText] = useState('{}');
  const [maxCharge, setMaxCharge] = useState('');
  const [prepared, setPrepared] = useState<PreparedRunStart | null>(null);
  const [risk, setRisk] = useState<LaunchRiskReview | null>(null);
  const [result, setResult] = useState<StartedRun | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const effectiveCap = maxCharge || selected?.reserveMicro || '';
  const locked = prepared !== null || busy;

  const currentArguments = () => simpleFields ? simpleArguments(simpleFields, simpleValues) : parseArguments(argumentsText);

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
      const args = currentArguments();
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
      const args = currentArguments();
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
    <Empty><EmptyHeader><EmptyTitle>Sign in to run tools</EmptyTitle><EmptyDescription>Mender uses your workspace session to determine which tools and connections you may use.</EmptyDescription></EmptyHeader><EmptyContent><Button asChild><a href="/auth/login">Sign in</a></Button></EmptyContent></Empty>
  </>;

  return <>
    <div className="page-heading-row product-heading"><div><h1>Run a tool</h1><p className="lead">Choose a capability, provide its inputs, and review cost and risk before Mender starts the run.</p></div><Button asChild variant="outline"><Link to="/catalog">Back to tools</Link></Button></div>
    {workspaces.isPending && <Empty><EmptyHeader><EmptyTitle>Loading workspace…</EmptyTitle></EmptyHeader></Empty>}
    {workspaces.error && <Alert variant="destructive"><AlertTitle>Workspace unavailable</AlertTitle><AlertDescription>{message(workspaces.error)}</AlertDescription></Alert>}
    {workspaces.data && workspaces.data.length > 0 && <section className="launch-panel">
      <div className="launch-toolbar product-launch-toolbar">
        <Field><FieldLabel>Workspace</FieldLabel><Select disabled={locked} value={workspaceId} onValueChange={(value) => { setSelection(value); setOptionKey(''); setMaxCharge(''); resetRisk(); }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{workspaces.data.map((item) => <SelectItem key={item.id} value={item.id}>{item.id}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
        {selected && <div className="launch-price"><span>Maximum charge</span><strong>{money(selected.reserveMicro, selected.currency)}</strong></div>}
      </div>
      {options.isPending && <Empty><EmptyHeader><EmptyTitle>Loading tools…</EmptyTitle></EmptyHeader></Empty>}
      {options.error && <Alert variant="destructive"><AlertTitle>Tools unavailable</AlertTitle><AlertDescription>{message(options.error)}</AlertDescription></Alert>}
      {options.data && options.data.length === 0 && <Empty><EmptyHeader><EmptyTitle>No runnable tools yet</EmptyTitle><EmptyDescription>Publish a tool and grant this account access to its connection. It will appear here automatically.</EmptyDescription></EmptyHeader><EmptyContent><Button asChild variant="outline"><Link to="/publisher/catalog">Open Publisher</Link></Button></EmptyContent></Empty>}
      {selected && <div className="product-launch-grid">
        <Card className="launch-tool-card">
          <CardHeader><div className="tool-card-heading"><div><CardTitle>{selected.title}</CardTitle><CardDescription>{selected.description || selected.providerId}</CardDescription></div><Badge variant={selected.sideEffect === 'read_only' ? 'secondary' : 'outline'}>{selected.sideEffect === 'read_only' ? 'Read only' : 'Writes data'}</Badge></div></CardHeader>
          <CardContent className="flex flex-col gap-5">
            {options.data!.length > 1 && <Field><FieldLabel>Tool</FieldLabel><Select disabled={locked} value={launchOptionKey(selected)} onValueChange={(value) => { setOptionKey(value); setSimpleValues({}); setArgumentsText('{}'); setMaxCharge(''); resetRisk(); }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{options.data!.map((item) => <SelectItem key={launchOptionKey(item)} value={launchOptionKey(item)}>{item.title}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>}
            {simpleFields ? <FieldGroup>{simpleFields.map((field) => <Field key={field.name}><FieldLabel htmlFor={`launch-${field.name}`}>{field.label}</FieldLabel><Input id={`launch-${field.name}`} disabled={locked} required={field.required} inputMode={field.type === 'string' ? 'text' : 'decimal'} value={simpleValues[field.name] ?? ''} onChange={(event) => { setSimpleValues((current) => ({ ...current, [field.name]: event.target.value })); resetRisk(); }} />{field.description && <FieldDescription>{field.description}</FieldDescription>}</Field>)}</FieldGroup>
              : <Field><FieldLabel htmlFor="launch-arguments">Inputs (JSON)</FieldLabel><Textarea id="launch-arguments" disabled={locked} value={argumentsText} onChange={(event) => { setArgumentsText(event.target.value); resetRisk(); }} spellCheck={false} rows={10} /><FieldDescription>This tool uses a schema that cannot be represented as a simple form yet.</FieldDescription></Field>}
            <div className="launch-summary-row"><div><span>Connection</span><strong>{selected.connectionId}</strong></div><div><span>Version</span><strong>{selected.toolVersion}</strong></div></div>
            <details className="advanced-panel"><summary>Advanced</summary><div className="advanced-panel-body"><Field><FieldLabel>Maximum charge (micro units)</FieldLabel><Input disabled={locked} inputMode="numeric" value={effectiveCap} onChange={(event) => setMaxCharge(event.target.value)} /></Field><pre className="schema-preview">{JSON.stringify(selected.inputSchema, null, 2)}</pre></div></details>
          </CardContent>
        </Card>
        <div className="launch-run-column">
          <Card><CardHeader><CardTitle>Review & run</CardTitle><CardDescription>Mender checks access, budget, price and execution policy again when the run is submitted.</CardDescription></CardHeader><CardContent className="flex flex-col gap-4">
            {risk && <RiskStatus value={risk} />}
            {prepared && <Alert><AlertTitle>Previous response was uncertain</AlertTitle><AlertDescription>Retrying keeps the same idempotency key, so Mender will not intentionally create a second logical run.</AlertDescription></Alert>}
            {error !== null && <Alert variant="destructive"><AlertTitle>Run not started</AlertTitle><AlertDescription>{message(error)}</AlertDescription></Alert>}
            {result ? <Alert><AlertTitle>Run accepted</AlertTitle><AlertDescription>{result.replayed ? 'The same idempotent request was recovered.' : 'Your run was accepted and is being processed.'}</AlertDescription><Button asChild className="mt-3" variant="outline"><Link to={`/runs?workspace=${encodeURIComponent(workspaceId)}`}>View run</Link></Button></Alert>
              : <div className="launch-cta">{risk?.decision.outcome === 'confirmation_required' && !prepared ? <Button size="lg" type="button" disabled={busy || !effectiveCap} onClick={() => void confirmAndStart()}>{busy ? 'Confirming…' : 'Confirm and run'}</Button> : risk?.decision.outcome === 'deny' && !prepared ? <Button size="lg" type="button" disabled>Blocked by policy</Button> : <Button size="lg" type="button" disabled={busy || !effectiveCap} onClick={() => void previewAndMaybeStart()}>{busy ? 'Checking…' : prepared ? 'Retry safely' : 'Run tool'}</Button>}{prepared && <Button type="button" variant="ghost" disabled={busy} onClick={() => void resetPrepared()}>Reset</Button>}<span>{money(selected.reserveMicro, selected.currency)} maximum for this run</span></div>}
          </CardContent></Card>
        </div>
      </div>}
    </section>}
  </>;
}
