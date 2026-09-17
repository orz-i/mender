import { useEffect, useId, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Button } from './components/button';
import { Alert, AlertDescription, AlertTitle } from './components/alert';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './components/card';
import { Checkbox } from './components/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './components/dialog';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from './components/empty';
import { Field, FieldDescription, FieldGroup, FieldLabel, FieldSet } from './components/field';
import { Input } from './components/input';
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from './components/select';
import { Textarea } from './components/textarea';

// UI-only building blocks: callbacks are injected by each app's composition root.
// No business client, role decisions, state transitions or monetary calculations here.
export interface WorkbenchField { name: string; label: string; initial?: string; options?: string[]; multiline?: boolean; required?: boolean; readOnly?: boolean; maxLength?: number }
export interface WorkbenchAction { id: string; label: string; description: string; mutation: boolean; fields: WorkbenchField[]; requiresRecord?: boolean }
export interface WorkbenchRecord { key: string; title: string; state: string; facts: { label: string; value: string }[]; inputs: Record<string, string> }
export interface WorkbenchResult { records: WorkbenchRecord[]; note: string }
interface Props {
  title: string; eyebrow: string; description: string; boundary: string;
  workspaceScoped?: boolean; actions: WorkbenchAction[];
  session: (signal: AbortSignal) => Promise<{ userId: string }>;
  execute: (command: string, values: Record<string, string>, signal: AbortSignal) => Promise<WorkbenchResult>;
}
const message = (error: unknown) => error instanceof Error ? error.message : '请求未完成，请核对服务端状态。';

export function Workbench(props: Props) {
  const readSession = props.session;
  const [workspaceDraft, setWorkspaceDraft] = useState('');
  const [scope, setScope] = useState('');
  const [session, setSession] = useState<{ userId: string } | null>(null);
  const [sessionError, setSessionError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    readSession(controller.signal).then((value) => { if (!controller.signal.aborted) setSession(value); })
      .catch((error: unknown) => { if (!controller.signal.aborted) setSessionError(message(error)); });
    return () => controller.abort();
  }, [readSession]);
  return <>
    <div className="product-heading"><h1>{props.title}</h1><p className="lead">{props.description}</p></div>
    <Alert className="mb-5"><AlertTitle>Before you continue</AlertTitle><AlertDescription>{props.boundary}</AlertDescription></Alert>
    {sessionError ? <Alert variant="destructive"><AlertTitle>Sign in required</AlertTitle><AlertDescription>{sessionError} <a href="/auth/login">Sign in again</a></AlertDescription></Alert>
      : !session ? <p role="status">正在验证登录会话…</p> : <>
        <div className="workbench-identity">Signed in as <code>{session.userId}</code><span>Permissions are rechecked by the service for every operation.</span></div>
        {props.workspaceScoped && <form className="workbench-scope" onSubmit={(event) => {
          event.preventDefault(); if (/^[A-Za-z0-9_-]{1,128}$/.test(workspaceDraft)) setScope(workspaceDraft);
        }}><Field><FieldLabel htmlFor="workbench-workspace">Workspace</FieldLabel><Input id="workbench-workspace" name="workspace" required pattern="[A-Za-z0-9_-]{1,128}" maxLength={128} value={workspaceDraft} onChange={(event) => setWorkspaceDraft(event.target.value)} placeholder="Workspace ID" /></Field>
          <Button type="submit" variant="outline">Use workspace</Button><span>{scope ? `Current scope: ${scope}` : 'Choose a workspace before loading records.'}</span></form>}
        {(!props.workspaceScoped || scope) && <ScopedWorkbench key={`${session.userId}:${scope}`} {...props} scope={scope} />}
      </>}
  </>;
}

function ScopedWorkbench({ actions, execute, scope }: Props & { scope: string }) {
  const [selected, setSelected] = useState(actions[0]!.id);
  const action = actions.find((item) => item.id === selected)!;
  const [seed, setSeed] = useState<Record<string, string>>({});
  const [selectedRecord, setSelectedRecord] = useState<WorkbenchRecord | null>(null);
  const [version, setVersion] = useState(0);
  const [result, setResult] = useState<WorkbenchResult | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  const [confirmation, setConfirmation] = useState<{ action: WorkbenchAction; values: Record<string, string>; binding: WorkbenchRecord | null } | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  useEffect(() => () => controller.current?.abort(), []);
  async function perform(operation: string, values: Record<string, string>) {
    controller.current?.abort();
    const active = new AbortController(); controller.current = active;
    setBusy(true); setError(''); setResult(null);
    try {
      const next = await execute(operation, scope ? { ...values, workspace_id: scope } : values, active.signal);
      if (!active.signal.aborted) setResult(next);
    } catch (failure) {
      if (!active.signal.aborted) setError(`${message(failure)} 不会自动重发写请求；网络结果不明时先读取状态。`);
    } finally { if (!active.signal.aborted) setBusy(false); }
  }
  function submit(values: Record<string, string>) {
    if (action.requiresRecord && (!selectedRecord || !values.approval_id || selectedRecord.inputs.approval_id !== values.approval_id)) { setError('请先读取对应审批并选择“用于表单”，核对服务端精确绑定后再复核。'); return; }
    if (action.mutation) { setConfirmed(false); setConfirmation({ action, values, binding: action.requiresRecord ? selectedRecord : null }); }
    else void perform(action.id, values);
  }
  return <div className="workbench-grid">
    <Card className="workbench-actions" aria-label="Workflow actions">
      <CardHeader><CardTitle>Workflow</CardTitle><CardDescription>{action.description}</CardDescription></CardHeader>
      <CardContent className="flex flex-col gap-5">
        <Field><FieldLabel>Action</FieldLabel><Select value={selected} disabled={busy} onValueChange={(value) => { setSelected(value); setError(''); setConfirmation(null); }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{actions.map((item) => <SelectItem key={item.id} value={item.id}>{item.label}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
        <ActionFields key={`${action.id}:${version}`} action={action} seed={seed} busy={busy} submit={submit} />
        {busy && <p role="status" className="muted-copy">Waiting for the service…</p>}
        {error && <Alert variant="destructive"><AlertTitle>Request not completed</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      </CardContent>
    </Card>
    <Card className="workbench-results" aria-label="Server results">
      <CardHeader><CardTitle>Results</CardTitle><CardDescription>Only server-returned facts are shown here.</CardDescription></CardHeader>
      <CardContent className="flex flex-col gap-4">
      {!result ? <Empty><EmptyHeader><EmptyTitle>{busy ? 'Loading…' : 'No results yet'}</EmptyTitle><EmptyDescription>Choose a read action to load records. Select a record before running actions that depend on an exact revision.</EmptyDescription></EmptyHeader></Empty> : <>
        <p role="status" className="workbench-notice">{result.note}</p>
        {result.records.length === 0 && <Empty><EmptyHeader><EmptyTitle>No records in this scope</EmptyTitle></EmptyHeader></Empty>}
        {result.records.map((record) => <article className="workbench-record" key={record.key}>
          <div className="workbench-record-heading"><h3>{record.title}</h3><span className="state-badge">{record.state || '服务端记录'}</span></div>
          <details><summary>查看记录与精确绑定</summary><dl className="fact-grid">{record.facts.map((fact) => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}</dl></details>
          {Object.keys(record.inputs).length > 0 && <Button variant="outline" type="button" disabled={busy} onClick={() => { setSeed(record.inputs); setSelectedRecord(record); setVersion((current) => current + 1); }}>用于表单</Button>}
        </article>)}
        {result.records.length > 0 && <Button variant="outline" type="button" onClick={() => {
          const data = result.records.map(({ title, state, facts }) => ({ title, state, facts }));
          const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }));
          const anchor = document.createElement('a'); anchor.href = url; anchor.download = 'mender-current-page.json'; anchor.click(); setTimeout(() => URL.revokeObjectURL(url), 0);
        }}>导出本页安全 JSON</Button>}
      </>}
      </CardContent>
    </Card>
    <Dialog open={Boolean(confirmation)} onOpenChange={(open) => { if (!open) setConfirmation(null); }}>
      {confirmation && <DialogContent><DialogHeader><DialogTitle>Confirm: {confirmation.action.label}</DialogTitle><DialogDescription>Review the target and exact revision. This confirmation never replaces server authorization or an independent approval.</DialogDescription></DialogHeader>
        {scope && <p>Workspace：<strong>{scope}</strong></p>}
        {confirmation.binding && <><h3>已读取的精确审批绑定</h3><dl>{confirmation.binding.facts.map((fact) => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}</dl></>}
        <dl>{confirmation.action.fields.map((field) => <div key={field.name}><dt>{field.label}</dt><dd>{confirmation.values[field.name] || '—'}</dd></div>)}</dl>
        <Field className="workbench-checkbox" data-disabled={busy || undefined}><div className="flex items-start gap-2"><Checkbox id="workbench-confirm" checked={confirmed} disabled={busy} onCheckedChange={(value) => setConfirmed(value === true)} /><FieldLabel htmlFor="workbench-confirm">I reviewed this operation and understand that submitting it does not guarantee final activation.</FieldLabel></div></Field>
        <DialogFooter><Button type="button" disabled={!confirmed || busy} onClick={() => {
          const pending = confirmation; setConfirmation(null); void perform(pending.action.id, pending.values);
        }}>Confirm</Button><Button type="button" variant="outline" onClick={() => setConfirmation(null)}>Cancel</Button></DialogFooter>
      </DialogContent>}
    </Dialog>
  </div>;
}

function ActionFields({ action, seed, busy, submit }: { action: WorkbenchAction; seed: Record<string, string>; busy: boolean; submit: (values: Record<string, string>) => void }) {
  const id = useId();
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(action.fields.map((field) => [field.name,
    field.readOnly ? field.initial ?? '' : field.options && !field.options.includes(seed[field.name] ?? '') ? field.initial ?? field.options[0] ?? '' : seed[field.name] ?? field.initial ?? field.options?.[0] ?? ''])));
  function onSubmit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); submit({ ...values }); }
  function control(field: WorkbenchField) {
    const controlID = `${id}-${field.name}`;
    if (field.options) return <Select value={values[field.name]} disabled={field.readOnly} onValueChange={(value) => setValues({ ...values, [field.name]: value })}>
      <SelectTrigger id={controlID}><SelectValue /></SelectTrigger>
      <SelectContent><SelectGroup>{field.options.map((option) => <SelectItem key={option} value={option}>{option}</SelectItem>)}</SelectGroup></SelectContent>
    </Select>;
    if (field.multiline) return <Textarea id={controlID} rows={field.name === 'manifest' ? 13 : 3} required={field.required !== false} maxLength={field.maxLength ?? 32768} value={values[field.name]} readOnly={field.readOnly} onChange={(event) => setValues({ ...values, [field.name]: event.target.value })} />;
    return <Input id={controlID} required={field.required !== false} maxLength={field.maxLength ?? 256} value={values[field.name]} readOnly={field.readOnly} autoComplete="off" onChange={(event) => setValues({ ...values, [field.name]: event.target.value })} />;
  }
  return <form className="workbench-form" onSubmit={onSubmit}><FieldSet disabled={busy}><FieldGroup>
    {action.fields.map((field) => <Field key={field.name} data-disabled={field.readOnly || undefined}><FieldLabel htmlFor={`${id}-${field.name}`}>{field.label}</FieldLabel>
      {control(field)}
      {field.readOnly && <FieldDescription>Server-owned value</FieldDescription>}
    </Field>)}
    <Button type="submit" disabled={busy}>{action.mutation ? 'Review action' : 'Load records'}</Button>
  </FieldGroup></FieldSet></form>;
}
