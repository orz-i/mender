import { useRef, useState } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
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

const remoteStateFacts: Record<RunState, string> = {
  queued: '服务端已受理 Run，但尚未确认远程执行方已经接收任务。',
  running: '服务端已确认远程执行方受理。对于异步 Provider / Agent，这只表示任务已在上游运行，不代表已经完成。',
  waiting_input: '服务端明确投影为等待输入；只有服务端同时提供受保护的 supplemental input request 时，页面才允许按其 schema 提交一次输入。',
  cancel_requested: '服务端已经持久化取消意图，但远程 Provider / Agent 是否停止仍未确认。不要把该状态当作 canceled。',
  reconciling: '远程提交或结果存在不确定性，服务端正在收敛事实；Mender 不会把未知结果显示为成功，也不会盲目重发副作用请求。',
  succeeded: '服务端已经确认成功终态；受审远程结果通过既有 provider_result Artifact 暴露，而不是通过 Provider/Agent 内部任务句柄。',
  failed: '服务端已经确认失败终态；页面只展示该终态和事件事实，不从客户端推断上游原因。',
  canceled: '服务端已经确认取消终态；这与仅有 cancel_requested 的状态不同。',
  timed_out: '服务端已经确认超时终态；页面不会把超时自动解释为远程任务一定已停止。',
};

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请检查 Mender API 与当前凭据。';
}

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(date);
}

function formatMicro(value: string, currency: string) {
  const micro = BigInt(value);
  const whole = micro / 1_000_000n;
  const fraction = (micro % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '');
  return `${currency} ${whole}${fraction ? `.${fraction}` : ''}`;
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
  const [searchParams] = useSearchParams();
  const workspace = searchParams.get('workspace') ?? '';
  const [access, setAccess] = useState<ActiveAccess | null>(null);
  const accessRevision = useRef(0);
  const [state, setState] = useState<'' | RunState>('');
  const [cursor, setCursor] = useState<string | null>(null);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [selectedArtifact, setSelectedArtifact] = useState<string | null>(null);
  const [cancelReason, setCancelReason] = useState('');
  const [agentInputAnswer, setAgentInputAnswer] = useState('{}');

  const clearAccess = () => {
    void queryClient.cancelQueries({ queryKey: ['run-list'] });
    void queryClient.cancelQueries({ queryKey: ['run-detail'] });
    void queryClient.cancelQueries({ queryKey: ['run-events'] });
    void queryClient.cancelQueries({ queryKey: ['run-artifacts'] });
    void queryClient.cancelQueries({ queryKey: ['run-artifact-content'] });
    void queryClient.cancelQueries({ queryKey: ['run-artifact-object-status'] });
    void queryClient.cancelQueries({ queryKey: ['run-cost'] });
    void queryClient.cancelQueries({ queryKey: ['run-agent-input'] });
    queryClient.removeQueries({ queryKey: ['run-list'] });
    queryClient.removeQueries({ queryKey: ['run-detail'] });
    queryClient.removeQueries({ queryKey: ['run-events'] });
    queryClient.removeQueries({ queryKey: ['run-artifacts'] });
    queryClient.removeQueries({ queryKey: ['run-artifact-content'] });
    queryClient.removeQueries({ queryKey: ['run-artifact-object-status'] });
    queryClient.removeQueries({ queryKey: ['run-cost'] });
    queryClient.removeQueries({ queryKey: ['run-agent-input'] });
    setAccess(null); setSelectedRun(null); setSelectedArtifact(null); setCursor(null); setCancelReason(''); setAgentInputAnswer('{}');
  };

  const connectMutation = useMutation({
    mutationFn: async () => {
      if (!workspace) throw new Error('请先从 Workspace 页面选择一个 Workspace');
      return gateway.connect(workspace);
    },
    onSuccess: (delegation) => {
      accessRevision.current += 1;
      clearAccess();
      setAccess({ ...delegation, sessionKey: accessRevision.current });
    },
  });

  const agentInputMutation = useMutation({
    mutationFn: async () => {
      if (!access || !selectedRun || !agentInputQuery.data) throw new Error('当前 Run 没有可提交的 supplemental input');
      let answer: unknown;
      try { answer = JSON.parse(agentInputAnswer); } catch { throw new Error('Supplemental input 必须是有效 JSON object'); }
      if (typeof answer !== 'object' || answer === null || Array.isArray(answer)) throw new Error('Supplemental input 必须是 JSON object');
      return gateway.submitAgentInput(access, selectedRun, agentInputQuery.data.inputRequestId, answer as Record<string, unknown>);
    },
    onSuccess: async () => {
      setAgentInputAnswer('{}');
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['run-agent-input', access?.sessionKey] }),
        queryClient.invalidateQueries({ queryKey: ['run-detail', access?.sessionKey] }),
        queryClient.invalidateQueries({ queryKey: ['run-events', access?.sessionKey] }),
        queryClient.invalidateQueries({ queryKey: ['run-list', access?.sessionKey] }),
      ]);
    },
  });

  const agentInputQuery = useQuery({
    queryKey: ['run-agent-input', access?.sessionKey, access?.workspaceId, selectedRun],
    enabled: access !== null && selectedRun !== null,
    retry: false,
    queryFn: ({ signal }) => gateway.agentInput(access!, selectedRun!, signal),
  });

  const costQuery = useQuery({
    queryKey: ['run-cost', access?.sessionKey, access?.workspaceId, selectedRun],
    enabled: access !== null && selectedRun !== null,
    retry: false,
    queryFn: ({ signal }) => gateway.cost(access!, selectedRun!, signal),
  });

  const disconnectMutation = useMutation({
    mutationFn: async () => {
      if (!access) return;
      await gateway.disconnect(access);
    },
    onSuccess: clearAccess,
  });

  const listQuery = useQuery({
    queryKey: ['run-list', access?.sessionKey, access?.workspaceId, state, cursor],
    enabled: access !== null,
    queryFn: ({ signal }) => gateway.list(access!, { limit: 20, state: state || undefined, cursor: cursor || undefined }, signal),
  });

  const detailQuery = useQuery({
    queryKey: ['run-detail', access?.sessionKey, access?.workspaceId, selectedRun],
    enabled: access !== null && selectedRun !== null,
    queryFn: ({ signal }) => gateway.get(access!, selectedRun!, signal),
  });

  const eventsQuery = useInfiniteQuery({
    queryKey: ['run-events', access?.sessionKey, access?.workspaceId, selectedRun],
    enabled: access !== null && selectedRun !== null,
    initialPageParam: { cursor: undefined as string | undefined, expectedThroughVersion: undefined as string | undefined },
    queryFn: ({ signal, pageParam }) => gateway.events(access!, selectedRun!, { limit: 20, cursor: pageParam.cursor, expectedThroughVersion: pageParam.expectedThroughVersion }, signal),
    getNextPageParam: (lastPage) => lastPage.nextCursor ? { cursor: lastPage.nextCursor, expectedThroughVersion: lastPage.throughVersion } : undefined,
  });

  const artifactsQuery = useQuery({
    queryKey: ['run-artifacts', access?.sessionKey, access?.workspaceId, selectedRun],
    enabled: access !== null && selectedRun !== null,
    queryFn: ({ signal }) => gateway.artifacts(access!, selectedRun!, signal),
  });

  const artifactContentQuery = useQuery({
    queryKey: ['run-artifact-content', access?.sessionKey, access?.workspaceId, selectedRun, selectedArtifact],
    enabled: access !== null && selectedRun !== null && selectedArtifact !== null,
    queryFn: ({ signal }) => gateway.artifact(access!, selectedRun!, selectedArtifact!, signal),
  });

  const artifactObjectStatusQuery = useQuery({
    queryKey: ['run-artifact-object-status', access?.sessionKey, access?.workspaceId, selectedRun, selectedArtifact],
    enabled: access !== null && selectedRun !== null && selectedArtifact !== null,
    retry: false,
    queryFn: ({ signal }) => gateway.artifactObjectStatus(access!, selectedRun!, selectedArtifact!, signal),
  });

  const artifactObjectReadMutation = useMutation({
    mutationFn: async () => {
      if (!access || !selectedRun || !selectedArtifact) throw new Error('请先选择 Artifact');
      return gateway.artifactObjectContent(access, selectedRun, selectedArtifact);
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
        queryClient.invalidateQueries({ queryKey: ['run-events', access?.sessionKey] }),
        queryClient.invalidateQueries({ queryKey: ['run-cost', access?.sessionKey] }),
      ]);
    },
  });

  const detail = detailQuery.data;
  const eventPages = eventsQuery.data?.pages ?? [];
  const events = eventPages.flatMap((page) => page.items);
  const eventThroughVersion = eventPages[0]?.throughVersion;
  const artifacts = artifactsQuery.data ?? [];
  const cost = costQuery.data;
  const agentInput = agentInputQuery.data;
  const canCancel = Boolean(detail && access?.canCancel && canRequestCancellation(detail.state));
  const canSubmitAgentInput = Boolean(detail?.state === 'waiting_input' && access?.canInput && agentInput?.state === 'pending');
  const activeNote = access ? `短期委托至 ${formatTime(access.expiresAt)}` : '尚未建立短期委托';

  return <>
    <p className="eyebrow">Mender / Run Explorer</p>
    <div className="page-heading-row"><div><h1>运行记录</h1><p className="lead">查看受保护的 Run 生命周期、事件与结果元数据。HTTP Tool 与受审 Remote Agent 共用同一 Run / Job / Artifact 真源；页面不会根据 Provider ID 或结果内容猜测上游终态。</p></div><span className="connection-state">{activeNote}</span></div>

    <section className="credential-panel" aria-labelledby="run-access-heading">
      <div><h2 id="run-access-heading">短期 Run 委托</h2><p>浏览器先使用 HttpOnly 人类会话取得当前 Workspace 权限，再显式申请短时 Run delegation。OIDC Cookie 本身不能读取、取消或提交 supplemental input；delegation token 只保存在当前页面内存并自动过期。</p></div>
      <div className="credential-form run-delegation-form">
        <div><span className="section-kicker">Workspace</span><strong className="mono">{workspace || '未选择'}</strong><p className="muted-copy">{access?.canInput ? '当前委托：读取 + 取消 + supplemental input' : access?.canCancel ? '当前委托：读取 + 取消' : access ? '当前委托：只读' : '权限范围会按当前 Membership 由服务端重新裁决。'}</p></div>
        <div className="credential-actions">{!access ? <Button type="button" disabled={!workspace || connectMutation.isPending} onClick={() => connectMutation.mutate()}>{connectMutation.isPending ? '正在建立…' : '建立短期访问'}</Button> : <Button type="button" variant="outline" disabled={disconnectMutation.isPending} onClick={() => disconnectMutation.mutate()}>{disconnectMutation.isPending ? '正在撤销…' : '撤销短期访问'}</Button>}<Button asChild type="button" variant="outline"><Link to="/workspaces">切换 Workspace</Link></Button></div>
        {connectMutation.error && <div className="error-panel" role="alert">{errorMessage(connectMutation.error)}</div>}
        {disconnectMutation.error && <div className="error-panel" role="alert">{errorMessage(disconnectMutation.error)}</div>}
      </div>
    </section>

    <section className="explorer-grid" aria-label="Run Explorer">
      <div className="run-list-panel">
        <div className="panel-heading"><div><p className="section-kicker">Execution</p><h2>Runs</h2></div><div className="run-controls">
          <label>状态<span className="sr-only">筛选</span><select value={state} onChange={(event) => { setState(event.target.value as '' | RunState); setCursor(null); setSelectedRun(null); }} disabled={!access}><>{stateOptions.map((item) => <option key={item.value || 'all'} value={item.value}>{item.label}</option>)}</></select></label>
          <Button type="button" variant="outline" disabled={!access || listQuery.isFetching} onClick={() => void listQuery.refetch()}>刷新</Button>
        </div></div>
        {!access && <div className="empty-state"><strong>建立短期委托后读取 Run</strong><span>没有 Machine Key 输入，也不会把 OIDC Cookie 当作 Run 权限。</span></div>}
        {access && listQuery.isPending && <div className="empty-state"><strong>正在读取 Run…</strong></div>}
        {listQuery.error && <div className="error-panel" role="alert">{errorMessage(listQuery.error)}</div>}
        {listQuery.data && <RunList items={listQuery.data.items} selected={selectedRun} onSelect={(id) => { setSelectedRun(id); setSelectedArtifact(null); setAgentInputAnswer('{}'); }} />}
        {listQuery.data && <div className="pagination-row"><span>每页最多 20 项</span><div>{cursor && <Button type="button" variant="outline" onClick={() => { setCursor(null); setSelectedRun(null); }}>回到第一页</Button>} {listQuery.data.nextCursor && <Button type="button" variant="outline" onClick={() => { setCursor(listQuery.data.nextCursor); setSelectedRun(null); }}>下一页</Button>}</div></div>}
      </div>

      <aside className="run-detail-panel" aria-label="Run 详情">
        {!selectedRun && <div className="empty-state"><strong>选择一个 Run</strong><span>详情、事件和 Artifact 元数据会并行读取。</span></div>}
        {selectedRun && (detailQuery.isPending || eventsQuery.isPending || artifactsQuery.isPending) && <div className="empty-state"><strong>正在读取详情…</strong></div>}
        {(detailQuery.error || eventsQuery.error || artifactsQuery.error) && <div className="error-panel" role="alert">{errorMessage(detailQuery.error ?? eventsQuery.error ?? artifactsQuery.error)}</div>}
        {detail && <>
          <div className="detail-title"><div><p className="section-kicker">Run</p><h2 className="mono">{detail.id}</h2></div><span className={`state-badge state-${detail.state}`}>{stateLabel(detail.state)}</span></div>
          <div className="success-panel" role="note"><strong>服务端远程执行事实</strong><span>{remoteStateFacts[detail.state]}</span></div>
          <dl className="fact-grid"><div><dt>版本</dt><dd>v{detail.version}</dd></div><div><dt>创建</dt><dd>{formatTime(detail.createdAt)}</dd></div><div><dt>更新</dt><dd>{formatTime(detail.updatedAt)}</dd></div><div><dt>事件快照</dt><dd>{eventThroughVersion ? `v${eventThroughVersion}` : '—'}</dd></div></dl>
          <section className="detail-section"><div className="detail-section-heading"><h3>Remote Agent supplemental input</h3><span className="muted-copy">服务端 request/schema 真源</span></div>
            {agentInputQuery.isPending && <p className="muted-copy">正在读取 supplemental input 请求…</p>}
            {agentInputQuery.error && <div className="error-panel" role="alert">{errorMessage(agentInputQuery.error)}</div>}
            {!agentInputQuery.isPending && !agentInputQuery.error && !agentInput && <p className="muted-copy">该 Run 没有可读的 supplemental input 请求。页面不会从 waiting_input 文案或 Provider 信息自行构造请求。</p>}
            {agentInput && <div className="empty-state compact"><strong>{agentInput.prompt}</strong><span>request <span className="mono">{agentInput.inputRequestId}</span> · state {agentInput.state} · {formatTime(agentInput.updatedAt)}</span><details><summary>服务端 JSON Schema</summary><pre>{JSON.stringify(agentInput.inputSchema, null, 2)}</pre></details>
              {canSubmitAgentInput ? <><label className="cancel-reason">JSON object answer<textarea value={agentInputAnswer} onChange={(event) => setAgentInputAnswer(event.target.value)} maxLength={65536} spellCheck={false} placeholder='{"region":"eu"}' /></label><Button type="button" variant="outline" disabled={agentInputMutation.isPending} onClick={() => agentInputMutation.mutate()}>{agentInputMutation.isPending ? '正在提交…' : '提交 supplemental input'}</Button></> : <span>只有服务端 request state=pending、Run=waiting_input 且委托包含 run:input 时才允许提交。sending/unknown 不会提供“重试发送”按钮。</span>}
              {agentInputMutation.error && <span className="error-panel" role="alert">{errorMessage(agentInputMutation.error)}</span>}
            </div>}
          </section>
          <section className="detail-section quota-cost-section"><div className="detail-section-heading"><h3>Quota cost</h3><span className="muted-copy">quota_only · 非支付账务</span></div>
            {costQuery.isPending && <p className="muted-copy">正在读取 reservation / settlement 事实…</p>}
            {costQuery.error && <div className="error-panel" role="alert">{errorMessage(costQuery.error)}</div>}
            {!costQuery.isPending && !costQuery.error && cost === null && <p className="muted-copy">该 Run 没有可读的 quota cost 投影，或当前部署未启用 Usage observability。</p>}
            {cost && <dl className="quota-cost-grid">
              <div><dt>Quota state</dt><dd><span className={`quota-badge quota-${cost.quotaState}`}>{cost.quotaState}</span></dd></div>
              <div><dt>Reserved</dt><dd>{formatMicro(cost.reservedMicro, cost.currency)}</dd></div>
              <div><dt>Charged</dt><dd>{cost.chargedMicro === null ? '待 settlement' : formatMicro(cost.chargedMicro, cost.currency)}</dd></div>
              <div><dt>Released</dt><dd>{formatMicro(cost.releasedMicro, cost.currency)}</dd></div>
              <div><dt>Budget</dt><dd className="mono">{cost.budgetId} / {cost.periodId}</dd></div>
              <div><dt>Outcome</dt><dd>{cost.outcome ?? '—'}</dd></div>
            </dl>}
          </section>
          <section className="detail-section"><div className="detail-section-heading"><h3>事件时间线</h3>{eventThroughVersion && <span className="muted-copy">固定到 v{eventThroughVersion}</span>}</div>{events.length === 0 ? <p className="muted-copy">当前没有后续状态事件。</p> : <ol className="event-list">{events.map((event) => <li key={event.version}><span className="event-dot" aria-hidden="true" /><div><strong>{stateLabel(event.state)}</strong><span>{formatTime(event.occurredAt)} · v{event.version}</span>{event.reason && <p>{event.reason}</p>}</div></li>)}</ol>}{eventsQuery.hasNextPage && <div className="result-actions"><Button type="button" variant="outline" disabled={eventsQuery.isFetchingNextPage} onClick={() => void eventsQuery.fetchNextPage()}>{eventsQuery.isFetchingNextPage ? '正在读取…' : '加载更多事件'}</Button><span className="muted-copy">更多页继续使用首屏 through-version；刷新详情才观察更新事件。</span></div>}</section>
          <section className="detail-section"><h3>Artifacts</h3>{artifacts.length === 0 ? <p className="muted-copy">尚无结果 Artifact。异步远程任务未确认成功时不会因为浏览器推断而出现结果。</p> : <ul className="artifact-list artifact-select-list">{artifacts.map((artifact) => <li key={artifact.id} className={selectedArtifact === artifact.id ? 'selected' : undefined}><button type="button" className="artifact-button" onClick={() => setSelectedArtifact(artifact.id)}><div><strong>{artifact.kind === 'provider_result' ? '远程结果（provider_result）' : artifact.kind}</strong><span className="mono">{artifact.id}</span></div><span>{artifact.mediaType} · {artifact.sizeBytes.toLocaleString()} B</span></button></li>)}</ul>}
            {artifactContentQuery.isPending && selectedArtifact && <div className="empty-state compact"><strong>正在读取 Artifact 内容…</strong></div>}
            {artifactContentQuery.error && <div className="error-panel" role="alert">{errorMessage(artifactContentQuery.error)}</div>}
            {artifactContentQuery.data && <div className="artifact-preview" aria-live="polite"><div className="detail-section-heading"><div><p className="section-kicker">Result JSON</p><strong className="mono">{artifactContentQuery.data.id}</strong></div><span>{artifactContentQuery.data.sizeBytes.toLocaleString()} B</span></div><pre>{JSON.stringify(artifactContentQuery.data.content, null, 2)}</pre></div>}
            {selectedArtifact && <div className="empty-state compact"><strong>Artifact object 副本</strong>
              {artifactObjectStatusQuery.isPending && <span>正在读取服务端对象生命周期事实…</span>}
              {artifactObjectStatusQuery.error && <span className="error-panel" role="alert">{errorMessage(artifactObjectStatusQuery.error)}</span>}
              {artifactObjectStatusQuery.data === null && <span>当前部署未启用短时对象读取；inline Artifact 仍是兼容真源。</span>}
              {artifactObjectStatusQuery.data?.state === 'not_materialized' && <span>服务端尚未物化对象副本。页面不会根据 Artifact 大小自行判断。</span>}
              {artifactObjectStatusQuery.data?.state === 'expired' && <span>服务端对象副本已过期（{artifactObjectStatusQuery.data.expiresAt ? formatTime(artifactObjectStatusQuery.data.expiresAt) : '—'}）；旧 capability 不再有效。</span>}
              {artifactObjectStatusQuery.data?.state === 'available' && <><span>服务端对象副本可用至 {formatTime(artifactObjectStatusQuery.data.expiresAt!)}。读取会重新签发 ≤5 分钟 capability，并在服务端再次验证摘要与生命周期。</span><Button type="button" variant="outline" disabled={artifactObjectReadMutation.isPending} onClick={() => artifactObjectReadMutation.mutate()}>{artifactObjectReadMutation.isPending ? '正在签发并读取…' : '通过短时 capability 读取对象副本'}</Button></>}
              {artifactObjectReadMutation.error && <span className="error-panel" role="alert">{errorMessage(artifactObjectReadMutation.error)}</span>}
              {artifactObjectReadMutation.data?.artifactId === selectedArtifact && <div className="artifact-preview" aria-live="polite"><div className="detail-section-heading"><div><p className="section-kicker">Object copy JSON</p><strong className="mono">{selectedArtifact}</strong></div><span>capability 至 {formatTime(artifactObjectReadMutation.data.capabilityExpiresAt)}</span></div><pre>{JSON.stringify(artifactObjectReadMutation.data.content, null, 2)}</pre></div>}
            </div>}
          </section>
          <section className="detail-section cancel-section"><h3>取消</h3>{canCancel ? <><label className="cancel-reason">取消原因（可选）<textarea value={cancelReason} maxLength={500} onChange={(event) => setCancelReason(event.target.value)} placeholder="仅提交取消意图；不会在客户端假定 Remote Agent / Provider 已停止" /></label><Button type="button" variant="outline" disabled={cancelMutation.isPending} onClick={() => cancelMutation.mutate()}>{cancelMutation.isPending ? '正在请求…' : '请求取消'}</Button></> : <p className="muted-copy">当前状态或当前短期委托不允许新的取消请求。服务端仍会重新校验 Membership 与 delegation scope；只有服务端投影为 canceled 才表示取消终态已确认。</p>}{cancelMutation.error && <div className="error-panel" role="alert">{errorMessage(cancelMutation.error)}</div>}</section>
        </>}
      </aside>
    </section>
  </>;
}
