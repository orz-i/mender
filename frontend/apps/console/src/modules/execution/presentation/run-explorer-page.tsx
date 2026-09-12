import { useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button } from '@mender/ui';
import type { RunAccess, RunGateway } from '../application/run-gateway';
import { canRequestCancellation, type Run, type RunState } from '../domain/run';

interface ActiveAccess extends RunAccess { sessionKey: number }

const stateOptions: Array<{ value: '' | RunState; label: string }> = [
  { value: '', label: '全部状态' },
  { value: 'queued', label: '排队中' },
  { value: 'running', label: '执行中' },
  { value: 'waiting_input', label: '等待输入' },
  { value: 'cancel_requested', label: '取消处理中' },
  { value: 'reconciling', label: '对账中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
  { value: 'timed_out', label: '已超时' },
];

const stateLabels = Object.fromEntries(stateOptions.filter((item) => item.value).map((item) => [item.value, item.label])) as Record<RunState, string>;

function stateLabel(state: RunState) { return stateLabels[state] ?? state; }

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请检查 Mender API 与当前凭据。';
}

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(date);
}

function RunList({ items, selected, onSelect }: { items: Run[]; selected: string | null; onSelect: (id: string) => void }) {
  if (items.length === 0) return <div className="empty-state"><strong>当前筛选没有 Run</strong><span>新的调用受理后会出现在这里。</span></div>;
  return <div className="run-table-wrap"><table className="run-table">
    <thead><tr><th>Run</th><th>状态</th><th>版本</th><th>更新时间</th></tr></thead>
    <tbody>{items.map((item) => <tr key={item.id} className={selected === item.id ? 'selected' : undefined}>
      <td><button className="run-link" type="button" onClick={() => onSelect(item.id)}>{item.id}</button></td>
      <td><span className={`state-badge state-${item.state}`}>{stateLabel(item.state)}</span></td>
      <td className="mono">v{item.version}</td><td>{formatTime(item.updatedAt)}</td>
    </tr>)}</tbody>
  </table></div>;
}

export function RunExplorerPage({ gateway }: { gateway: RunGateway }) {
  const queryClient = useQueryClient();
  const [workspace, setWorkspace] = useState('');
  const [token, setToken] = useState('');
  const [access, setAccess] = useState<ActiveAccess | null>(null);
  const accessRevision = useRef(0);
  const [state, setState] = useState<'' | RunState>('');
  const [cursor, setCursor] = useState<string | null>(null);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [cancelReason, setCancelReason] = useState('');

  const listQuery = useQuery({
    queryKey: ['run-list', access?.sessionKey, access?.workspaceId, state, cursor],
    enabled: access !== null,
    queryFn: ({ signal }) => gateway.list(access!, { limit: 20, state: state || undefined, cursor: cursor || undefined }, signal),
  });

  const detailQuery = useQuery({
    queryKey: ['run-detail', access?.sessionKey, access?.workspaceId, selectedRun],
    enabled: access !== null && selectedRun !== null,
    queryFn: async ({ signal }) => {
      const current = access!;
      const runId = selectedRun!;
      const [run, events, artifacts] = await Promise.all([
        gateway.get(current, runId, signal), gateway.events(current, runId, signal), gateway.artifacts(current, runId, signal),
      ]);
      return { run, events, artifacts };
    },
  });

  const cancelMutation = useMutation({
    mutationFn: async () => {
      if (!access || !selectedRun) throw new Error('请先选择 Run');
      return gateway.cancel(access, selectedRun, cancelReason.trim());
    },
    onSuccess: async () => {
      setCancelReason('');
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['run-list', access?.sessionKey] }),
        queryClient.invalidateQueries({ queryKey: ['run-detail', access?.sessionKey] }),
      ]);
    },
  });

  const detail = detailQuery.data;
  const canCancel = detail ? canRequestCancellation(detail.run.state) : false;
  const activeNote = access ? `已连接 Workspace ${access.workspaceId}` : '尚未连接';

  return <>
    <p className="eyebrow">Mender / Run Explorer</p>
    <div className="page-heading-row"><div><h1>运行记录</h1><p className="lead">查看受保护的 Run 生命周期、事件与结果元数据，并通过现有权限模型请求取消。</p></div><span className="connection-state">{activeNote}</span></div>

    <section className="credential-panel" aria-labelledby="run-access-heading">
      <div><h2 id="run-access-heading">Alpha 访问凭据</h2><p>机器 Key 仅保存在当前页面内存，不写入 URL、localStorage 或 sessionStorage。刷新页面即清除。</p></div>
      <form className="credential-form" onSubmit={(event) => {
        event.preventDefault();
        const ws = workspace.trim(); const machineToken = token.trim();
        if (!ws || !machineToken) return;
        accessRevision.current += 1;
        setAccess({ workspaceId: ws, machineToken, sessionKey: accessRevision.current });
        setToken(''); setCursor(null); setSelectedRun(null);
      }}>
        <label>Workspace ID<input value={workspace} onChange={(event) => setWorkspace(event.target.value)} autoComplete="off" placeholder="ws_example" /></label>
        <label>Machine API Key<input type="password" value={token} onChange={(event) => setToken(event.target.value)} autoComplete="off" placeholder="仅当前页面使用" /></label>
        <div className="credential-actions"><Button type="submit" disabled={!workspace.trim() || !token.trim()}>连接并读取</Button>{access && <Button type="button" variant="outline" onClick={() => {
          void queryClient.cancelQueries({ queryKey: ['run-list'] });
          void queryClient.cancelQueries({ queryKey: ['run-detail'] });
          queryClient.removeQueries({ queryKey: ['run-list'] });
          queryClient.removeQueries({ queryKey: ['run-detail'] });
          setAccess(null); setSelectedRun(null); setCursor(null); setCancelReason('');
        }}>断开</Button>}</div>
      </form>
    </section>

    <section className="explorer-grid" aria-label="Run Explorer">
      <div className="run-list-panel">
        <div className="panel-heading"><div><p className="section-kicker">Execution</p><h2>Runs</h2></div><div className="run-controls">
          <label>状态<span className="sr-only">筛选</span><select value={state} onChange={(event) => { setState(event.target.value as '' | RunState); setCursor(null); setSelectedRun(null); }} disabled={!access}><>{stateOptions.map((item) => <option key={item.value || 'all'} value={item.value}>{item.label}</option>)}</></select></label>
          <Button type="button" variant="outline" disabled={!access || listQuery.isFetching} onClick={() => void listQuery.refetch()}>刷新</Button>
        </div></div>
        {!access && <div className="empty-state"><strong>输入访问凭据以读取 Run</strong><span>Console 不会保存机器 Key。</span></div>}
        {access && listQuery.isPending && <div className="empty-state"><strong>正在读取 Run…</strong></div>}
        {listQuery.error && <div className="error-panel" role="alert">{errorMessage(listQuery.error)}</div>}
        {listQuery.data && <RunList items={listQuery.data.items} selected={selectedRun} onSelect={setSelectedRun} />}
        {listQuery.data && <div className="pagination-row"><span>每页最多 20 项</span><div>{cursor && <Button type="button" variant="outline" onClick={() => { setCursor(null); setSelectedRun(null); }}>回到第一页</Button>} {listQuery.data.nextCursor && <Button type="button" variant="outline" onClick={() => { setCursor(listQuery.data.nextCursor); setSelectedRun(null); }}>下一页</Button>}</div></div>}
      </div>

      <aside className="run-detail-panel" aria-label="Run 详情">
        {!selectedRun && <div className="empty-state"><strong>选择一个 Run</strong><span>详情、事件和 Artifact 元数据会并行读取。</span></div>}
        {selectedRun && detailQuery.isPending && <div className="empty-state"><strong>正在读取详情…</strong></div>}
        {detailQuery.error && <div className="error-panel" role="alert">{errorMessage(detailQuery.error)}</div>}
        {detail && <>
          <div className="detail-title"><div><p className="section-kicker">Run</p><h2 className="mono">{detail.run.id}</h2></div><span className={`state-badge state-${detail.run.state}`}>{stateLabel(detail.run.state)}</span></div>
          <dl className="fact-grid"><div><dt>版本</dt><dd>v{detail.run.version}</dd></div><div><dt>创建</dt><dd>{formatTime(detail.run.createdAt)}</dd></div><div><dt>更新</dt><dd>{formatTime(detail.run.updatedAt)}</dd></div><div><dt>事件读到</dt><dd>v{detail.events.throughVersion}</dd></div></dl>
          <section className="detail-section"><h3>事件时间线</h3>{detail.events.items.length === 0 ? <p className="muted-copy">当前没有后续状态事件。</p> : <ol className="event-list">{detail.events.items.map((event) => <li key={event.version}><span className="event-dot" aria-hidden="true" /><div><strong>{stateLabel(event.state)}</strong><span>{formatTime(event.occurredAt)} · v{event.version}</span>{event.reason && <p>{event.reason}</p>}</div></li>)}</ol>}{detail.events.nextCursor && <p className="muted-copy">当前只展示前 100 条事件；更多事件留待后续分页界面。</p>}</section>
          <section className="detail-section"><h3>Artifacts</h3>{detail.artifacts.length === 0 ? <p className="muted-copy">尚无结果 Artifact。</p> : <ul className="artifact-list">{detail.artifacts.map((artifact) => <li key={artifact.id}><div><strong>{artifact.kind}</strong><span className="mono">{artifact.id}</span></div><span>{artifact.mediaType} · {artifact.sizeBytes.toLocaleString()} B</span></li>)}</ul>}</section>
          <section className="detail-section cancel-section"><h3>取消</h3>{canCancel ? <><label className="cancel-reason">取消原因（可选）<textarea value={cancelReason} maxLength={500} onChange={(event) => setCancelReason(event.target.value)} placeholder="不会在客户端推断取消是否已经到达上游" /></label><Button type="button" variant="outline" disabled={cancelMutation.isPending} onClick={() => cancelMutation.mutate()}>{cancelMutation.isPending ? '正在请求…' : '请求取消'}</Button></> : <p className="muted-copy">当前状态不提供新的取消请求。服务端仍是最终裁决者。</p>}{cancelMutation.error && <div className="error-panel" role="alert">{errorMessage(cancelMutation.error)}</div>}</section>
        </>}
      </aside>
    </section>
  </>;
}
