import { useEffect, useId, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Button } from './components/button';

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
    <p className="eyebrow">{props.eyebrow}</p><h1>{props.title}</h1><p className="lead">{props.description}</p>
    <div className="success-panel" role="note"><strong>操作边界</strong><span>{props.boundary}</span></div>
    {sessionError ? <div className="error-panel" role="alert">{sessionError} <a href="/auth/login">重新登录</a></div>
      : !session ? <p role="status">正在验证登录会话…</p> : <>
        <div className="workbench-identity">当前用户 <code>{session.userId}</code><span>会话不等于操作授权；权限每次由服务端复核。</span></div>
        {props.workspaceScoped && <form className="workbench-scope" onSubmit={(event) => {
          event.preventDefault(); if (/^[A-Za-z0-9_-]{1,128}$/.test(workspaceDraft)) setScope(workspaceDraft);
        }}><label>目标 Workspace<input name="workspace" required pattern="[A-Za-z0-9_-]{1,128}" maxLength={128} value={workspaceDraft} onChange={(event) => setWorkspaceDraft(event.target.value)} placeholder="Workspace ID" /></label>
          <Button type="submit" variant="outline">切换工作区</Button><span>{scope ? `当前范围：${scope}` : '先确认目标工作区；切换会清空结果与待确认操作。'}</span></form>}
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
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => () => controller.current?.abort(), []);
  useEffect(() => {
    if (confirmation) dialog.current?.showModal(); else dialog.current?.close();
  }, [confirmation]);
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
    <section className="workbench-actions" aria-label="工作流操作">
      <h2>工作流</h2><label>选择操作<select value={selected} disabled={busy} onChange={(event) => { setSelected(event.target.value); setError(''); setConfirmation(null); }}>{actions.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select></label>
      <p className="muted-copy">{action.description}</p>
      <ActionFields key={`${action.id}:${version}`} action={action} seed={seed} busy={busy} submit={submit} />
      {busy && <p role="status">正在等待服务端结果…</p>}
      {error && <div className="error-panel" role="alert">{error}</div>}
    </section>
    <section className="workbench-results" aria-label="服务器结果">
      <h2>服务器结果</h2>
      {!result ? <div className="empty-state"><strong>{busy ? '正在读取…' : '尚无结果'}</strong><span>选择列表操作读取真实记录，再使用记录填写操作表单。</span></div> : <>
        <p role="status" className="workbench-notice">{result.note}</p>
        {result.records.length === 0 && <div className="empty-state"><strong>当前范围没有记录</strong></div>}
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
    </section>
    <dialog ref={dialog} className="workbench-dialog" onCancel={() => setConfirmation(null)} aria-labelledby="workbench-confirm-title">
      {confirmation && <><h2 id="workbench-confirm-title">确认：{confirmation.action.label}</h2><p>核对目标、版本、理由与审批。该确认不会代替服务端授权或独立复核。</p>
        {scope && <p>Workspace：<strong>{scope}</strong></p>}
        {confirmation.binding && <><h3>已读取的精确审批绑定</h3><dl>{confirmation.binding.facts.map((fact) => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}</dl></>}
        <dl>{confirmation.action.fields.map((field) => <div key={field.name}><dt>{field.label}</dt><dd>{confirmation.values[field.name] || '—'}</dd></div>)}</dl>
        <label className="workbench-checkbox"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} />我已核对本次操作，不将请求提交视为最终生效。</label>
        <div className="result-actions"><Button type="button" disabled={!confirmed || busy} onClick={() => {
          const pending = confirmation; setConfirmation(null); void perform(pending.action.id, pending.values);
        }}>确认执行</Button><Button type="button" variant="outline" onClick={() => setConfirmation(null)}>返回修改</Button></div>
      </>}
    </dialog>
  </div>;
}

function ActionFields({ action, seed, busy, submit }: { action: WorkbenchAction; seed: Record<string, string>; busy: boolean; submit: (values: Record<string, string>) => void }) {
  const id = useId();
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(action.fields.map((field) => [field.name,
    field.readOnly ? field.initial ?? '' : field.options && !field.options.includes(seed[field.name] ?? '') ? field.initial ?? field.options[0] ?? '' : seed[field.name] ?? field.initial ?? field.options?.[0] ?? ''])));
  function onSubmit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); submit({ ...values }); }
  return <form className="workbench-form" onSubmit={onSubmit}><fieldset disabled={busy}>
    {action.fields.map((field) => <label htmlFor={`${id}-${field.name}`} key={field.name}>{field.label}
      {field.options ? <select id={`${id}-${field.name}`} value={values[field.name]} disabled={field.readOnly} onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}>{field.options.map((option) => <option key={option}>{option}</option>)}</select>
        : field.multiline ? <textarea id={`${id}-${field.name}`} rows={field.name === 'manifest' ? 13 : 3} required={field.required !== false} maxLength={field.maxLength ?? 32768} value={values[field.name]} readOnly={field.readOnly} onChange={(event) => setValues({ ...values, [field.name]: event.target.value })} />
          : <input id={`${id}-${field.name}`} required={field.required !== false} maxLength={field.maxLength ?? 256} value={values[field.name]} readOnly={field.readOnly} autoComplete="off" onChange={(event) => setValues({ ...values, [field.name]: event.target.value })} />}
    </label>)}
    <Button type="submit" disabled={busy}>{action.mutation ? '核对操作' : '读取记录'}</Button>
  </fieldset></form>;
}
