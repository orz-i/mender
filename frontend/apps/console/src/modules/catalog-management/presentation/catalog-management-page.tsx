import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { Button } from '@mender/ui';
import { CatalogLoginRequiredError, CatalogPolicyDeniedError, type CatalogGateway } from '../application/catalog-gateway';
import { draftToolInput, latestApproval, type CatalogBindingInput, type CatalogPreflight, type CatalogToolVersion, type CatalogToolVersionInput, type PublicationApproval, type PublicationPolicyDecision } from '../domain/catalog';

function message(error: unknown) { return error instanceof Error ? error.message : 'Catalog 操作失败。'; }
function formatTime(value: string | null) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }
function parseObject(raw: string) { const value: unknown = JSON.parse(raw); if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Schema 必须是 JSON object。'); return value as Record<string, unknown>; }
const stateLabel = { draft: '草稿', published: '已发布', retired: '已退役' } as const;
const issueLabel: Record<string, string> = {
  not_found: '目标不存在', not_draft: '目标已不再是草稿', version_conflict: '版本标识已被占用', pricing_unavailable: '当前 PriceVersion 不可用',
  empty_toolset: 'Toolset 尚未绑定任何工具', tool_version_unpublished: '绑定的 ToolVersion 尚未发布', connection_unavailable: 'Connection 当前不可用',
  budget_unavailable: 'Budget period 当前不可用', mcp_contract_unavailable: '该 ToolVersion 不满足 MCP 暴露合同',
};
const approvalLabel = { pending: '等待审核', approved: '已批准', rejected: '已拒绝', consumed: '已消费', expired: '已过期/失效' } as const;

type ToolForm = Omit<CatalogToolVersionInput, 'inputSchema' | 'outputSchema'> & { inputSchemaText: string; outputSchemaText: string };
function formFromTool(tool?: CatalogToolVersion): ToolForm {
  const input = draftToolInput(tool);
  return { ...input, inputSchemaText: JSON.stringify(input.inputSchema, null, 2), outputSchemaText: JSON.stringify(input.outputSchema, null, 2) };
}
function PolicyDecisionStatus({ value }: { value: PublicationPolicyDecision }) {
  const denied = value.outcome === 'deny';
  return <div className={denied ? 'error-panel' : 'success-panel'} role="status"><strong>服务端 PolicyDecision：{denied ? 'Deny' : 'Allow'} · risk {value.riskLevel}</strong><span>policy revision {value.policyRevision} · target revision {value.targetRevision} · decision <span className="mono">{value.sequence}</span></span><span>{value.reasonCodes.join(' · ')}</span></div>;
}

function ApprovalStatus({ value }: { value: PublicationApproval }) {
  return <div className={value.state === 'approved' ? 'success-panel' : value.state === 'rejected' || value.state === 'expired' ? 'error-panel' : 'empty-state'} role="status"><strong>发布审核：{approvalLabel[value.state]}</strong><span>approval <span className="mono">{value.id}</span> · target revision {value.targetRevision}</span>{value.reviewerUserId && <span>Reviewer：<span className="mono">{value.reviewerUserId}</span>{value.decisionNote ? ` · ${value.decisionNote}` : ''}</span>}</div>;
}
function toToolInput(form: ToolForm): CatalogToolVersionInput {
  return { ...form, inputSchema: parseObject(form.inputSchemaText), outputSchema: parseObject(form.outputSchemaText) };
}

export function CatalogManagementPage({ gateway }: { gateway: CatalogGateway }) {
  const queryClient = useQueryClient();
  const [params] = useSearchParams();
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const workspaces = useQuery({ queryKey: ['catalog-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const snapshot = useQuery({ queryKey: ['console-catalog', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.snapshot(workspaceId, signal), retry: false });
  const [toolForm, setToolForm] = useState<ToolForm>(() => formFromTool());
  const [editingTool, setEditingTool] = useState<string | null>(null);
  const [toolsetId, setToolsetId] = useState('');
  const [bindingToolset, setBindingToolset] = useState('');
  const [bindingToolVersion, setBindingToolVersion] = useState('');
  const [bindingBudget, setBindingBudget] = useState('');
  const [bindingConnection, setBindingConnection] = useState('');
  const [bindingMCPName, setBindingMCPName] = useState('');
  const [bindingMCPExposed, setBindingMCPExposed] = useState(false);
  const [preflights, setPreflights] = useState<Record<string, CatalogPreflight>>({});
  const [policyDecisions, setPolicyDecisions] = useState<Record<string, PublicationPolicyDecision>>({});
  const [busy, setBusy] = useState('');
  const [actionError, setActionError] = useState<unknown>(null);
  const [notice, setNotice] = useState('');

  const tools = snapshot.data?.toolVersions ?? [];
  const toolsets = useMemo(() => snapshot.data?.toolsets ?? [], [snapshot.data?.toolsets]);
  const draftToolsets = useMemo(() => toolsets.filter((item) => item.state === 'draft'), [toolsets]);
  const selectedBindingTool = tools.find((item) => item.toolVersionId === bindingToolVersion) ?? null;
  const selectedBindingSet = bindingToolset || draftToolsets[0]?.id || '';
  const selectedConnection = bindingConnection || snapshot.data?.connections[0]?.connectionId || '';
  const selectedBudget = bindingBudget || snapshot.data?.budgetPeriods[0]?.budgetId || '';

  async function refresh() { await queryClient.invalidateQueries({ queryKey: ['console-catalog', workspaceId] }); }
  async function act(key: string, operation: () => Promise<void>, success: string) {
    setBusy(key); setActionError(null); setNotice('');
    try { await operation(); await refresh(); setNotice(success); }
    catch (error) { setActionError(error); }
    finally { setBusy(''); }
  }
  function clearPreflight(key: string) { setPreflights((current) => { const next = { ...current }; delete next[key]; return next; }); }
  function clearPolicyDecision(key: string) { setPolicyDecisions((current) => { const next = { ...current }; delete next[key]; return next; }); }

  const saveTool = async () => {
    let input: CatalogToolVersionInput;
    try { input = toToolInput(toolForm); } catch (error) { setActionError(error); return; }
    const key = `tool:${input.toolVersionId}`;
    await act('save-tool', async () => { if (editingTool) await gateway.updateToolVersion(workspaceId, input); else await gateway.createToolVersion(workspaceId, input); clearPreflight(key); clearPolicyDecision(key); setEditingTool(null); setToolForm(formFromTool()); }, editingTool ? 'ToolVersion 草稿已更新。' : 'ToolVersion 草稿已创建。');
  };
  async function requestReview(kind: 'tool' | 'set', id: string) {
    const key = `${kind}:${id}`; setBusy(`review-${kind}:${id}`); setActionError(null); setNotice('');
    try {
      const result = kind === 'tool' ? await gateway.requestToolVersionReview(workspaceId, id) : await gateway.requestToolsetReview(workspaceId, id);
      setPolicyDecisions((current) => ({ ...current, [key]: result.policyDecision })); clearPreflight(key); await refresh(); setNotice(`${kind === 'tool' ? 'ToolVersion' : 'Toolset'} 发布审核已提交；PolicyDecision 为 ${result.policyDecision.outcome}。`);
    } catch (error) {
      if (error instanceof CatalogPolicyDeniedError) setPolicyDecisions((current) => ({ ...current, [key]: error.decision }));
      setActionError(error);
    } finally { setBusy(''); }
  }
  const requestToolReview = async (id: string) => requestReview('tool', id);
  const requestToolsetReview = async (id: string) => requestReview('set', id);
  const preflightTool = async (id: string) => {
    setBusy(`preflight-tool:${id}`); setActionError(null); setNotice('');
    try { const result = await gateway.toolVersionPreflight(workspaceId, id); setPreflights((current) => ({ ...current, [`tool:${id}`]: result })); }
    catch (error) { setActionError(error); }
    finally { setBusy(''); }
  };
  const preflightToolset = async (id: string) => {
    setBusy(`preflight-set:${id}`); setActionError(null); setNotice('');
    try { const result = await gateway.toolsetPreflight(workspaceId, id); setPreflights((current) => ({ ...current, [`set:${id}`]: result })); }
    catch (error) { setActionError(error); }
    finally { setBusy(''); }
  };
  const saveBinding = async () => {
    if (!selectedBindingSet || !selectedBindingTool) { setActionError(new Error('请选择 draft Toolset 与 ToolVersion。')); return; }
    const input: CatalogBindingInput = { toolId: selectedBindingTool.toolId, toolVersionLabel: selectedBindingTool.version, budgetId: selectedBudget, connectionId: selectedConnection, mcpName: bindingMCPName, mcpExposed: bindingMCPExposed };
    await act('save-binding', async () => { await gateway.upsertBinding(workspaceId, selectedBindingSet, selectedBindingTool.toolVersionId, input); clearPreflight(`set:${selectedBindingSet}`); clearPolicyDecision(`set:${selectedBindingSet}`); }, 'Toolset binding 已保存为草稿事实。');
  };

  if (workspaces.error instanceof CatalogLoginRequiredError || snapshot.error instanceof CatalogLoginRequiredError) return <>
    <p className="eyebrow">Mender / Catalog</p><h1>登录 Console</h1><p className="lead">Catalog 自助管理通过受保护的人类会话与 Workspace membership 授权。</p><Button asChild><a href="/auth/login">使用 OIDC 登录</a></Button>
  </>;

  return <>
    <p className="eyebrow">Mender / Catalog</p>
    <div className="page-heading-row"><div><h1>Catalog / Toolset</h1><p className="lead">创建与编辑 draft，完成服务端 Preflight 后提交发布审核。Admin reviewer 批准精确 revision 后，Publish 仍会在事务中重新校验并原子消费 approval。</p></div></div>
    <div className="success-panel" role="note"><strong>发布资格、风险与审核状态都由服务端裁决</strong><span>页面不根据 side-effect、idempotency、Membership、Connection、Budget 或 Pricing 自行计算 risk/allow/deny；只消费服务端 Preflight、PolicyDecision 与 publication approval。</span></div>
    {workspaces.isPending && <div className="empty-state"><strong>正在读取 Workspace…</strong></div>}
    {workspaces.error && !(workspaces.error instanceof CatalogLoginRequiredError) && <div className="error-panel" role="alert">{message(workspaces.error)}</div>}
    {workspaces.data && workspaces.data.length === 0 && <div className="empty-state"><strong>没有可访问 Workspace</strong></div>}
    {workspaces.data && workspaces.data.length > 0 && <>
      <section className="run-list-panel" aria-label="Catalog workspace selector"><div className="panel-heading"><div><p className="section-kicker">Workspace</p><h2>管理边界</h2></div><div className="run-controls"><label>Workspace<select value={workspaceId} onChange={(event) => { setSelection(event.target.value); setPreflights({}); setPolicyDecisions({}); setEditingTool(null); setToolForm(formFromTool()); }}>{workspaces.data.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.role}</option>)}</select></label><Button type="button" variant="outline" disabled={snapshot.isFetching} onClick={() => void snapshot.refetch()}>刷新</Button></div></div></section>
      {snapshot.isPending && <div className="empty-state"><strong>正在读取 Catalog facts…</strong></div>}
      {snapshot.error && !(snapshot.error instanceof CatalogLoginRequiredError) && <div className="error-panel" role="alert">{message(snapshot.error)}</div>}
      {actionError && <div className="error-panel" role="alert">{message(actionError)}</div>}
      {notice && <div className="success-panel" role="status">{notice}</div>}
      {snapshot.data && <>
        <section className="launch-panel" aria-labelledby="tool-draft-heading">
          <div className="panel-heading"><div><p className="section-kicker">Catalog</p><h2 id="tool-draft-heading">ToolVersion 草稿</h2></div>{editingTool && <Button type="button" variant="outline" onClick={() => { setEditingTool(null); setToolForm(formFromTool()); }}>取消编辑</Button>}</div>
          <div className="launch-grid"><div className="launch-request">
            <label>ToolVersion ID<input value={toolForm.toolVersionId} disabled={editingTool !== null} onChange={(event) => setToolForm({ ...toolForm, toolVersionId: event.target.value })} /></label>
            <label>Tool ID<input value={toolForm.toolId} onChange={(event) => setToolForm({ ...toolForm, toolId: event.target.value })} /></label>
            <label>Version<input value={toolForm.version} onChange={(event) => setToolForm({ ...toolForm, version: event.target.value })} /></label>
            <label>Title<input value={toolForm.title} onChange={(event) => setToolForm({ ...toolForm, title: event.target.value })} /></label>
            <label>Description<textarea rows={4} value={toolForm.description} onChange={(event) => setToolForm({ ...toolForm, description: event.target.value })} /></label>
            <label>Provider ID<input list="catalog-providers" value={toolForm.providerId} onChange={(event) => setToolForm({ ...toolForm, providerId: event.target.value })} /></label><datalist id="catalog-providers">{snapshot.data.connections.map((item) => <option key={`${item.connectionId}:${item.providerId}`} value={item.providerId} />)}</datalist>
            <label>PriceVersion<select value={toolForm.priceVersionId} onChange={(event) => setToolForm({ ...toolForm, priceVersionId: event.target.value })}><option value="">选择服务端 PriceVersion</option>{snapshot.data.priceVersions.map((item) => <option key={item.id} value={item.id}>{item.id} · {item.toolVersionId} · {item.currency} {item.reserveMicro} micro</option>)}</select></label>
            <label>Deployment revision<input value={toolForm.deploymentRevision} onChange={(event) => setToolForm({ ...toolForm, deploymentRevision: event.target.value })} /></label>
            <label>Side effect<select value={toolForm.sideEffect} onChange={(event) => setToolForm({ ...toolForm, sideEffect: event.target.value as ToolForm['sideEffect'] })}><option value="read_only">read_only</option><option value="write">write</option></select></label>
            <label>Idempotency<select value={toolForm.idempotency} onChange={(event) => setToolForm({ ...toolForm, idempotency: event.target.value as ToolForm['idempotency'] })}><option value="safe_read">safe_read</option><option value="idempotent">idempotent</option><option value="unsafe">unsafe</option></select></label>
            <label><input type="checkbox" checked={toolForm.mcpPublishable} onChange={(event) => setToolForm({ ...toolForm, mcpPublishable: event.target.checked })} /> MCP publishable</label>
          </div><div className="launch-request"><label>Input Schema JSON<textarea rows={13} spellCheck={false} value={toolForm.inputSchemaText} onChange={(event) => setToolForm({ ...toolForm, inputSchemaText: event.target.value })} /></label><label>Output Schema JSON<textarea rows={10} spellCheck={false} value={toolForm.outputSchemaText} onChange={(event) => setToolForm({ ...toolForm, outputSchemaText: event.target.value })} /></label><Button type="button" disabled={busy !== ''} onClick={() => void saveTool()}>{busy === 'save-tool' ? '正在保存…' : editingTool ? '保存 draft 修改' : '创建 ToolVersion draft'}</Button></div></div>
        </section>

        <section className="run-list-panel" aria-label="ToolVersion lifecycle"><div className="panel-heading"><div><p className="section-kicker">Versions</p><h2>ToolVersion 生命周期</h2></div></div>
          {tools.length === 0 ? <div className="empty-state"><strong>当前 Workspace 没有管理中的 ToolVersion</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>ToolVersion</th><th>Provider / Price</th><th>状态 / 审核</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{tools.map((tool) => { const key = `tool:${tool.toolVersionId}`; const pf = preflights[key]; const policy = policyDecisions[key]; const approval = latestApproval(snapshot.data.publicationApprovals, 'tool_version', tool.toolVersionId); return <tr key={tool.toolVersionId}><td><strong>{tool.title}</strong><div className="mono">{tool.toolId}@{tool.version}</div><div className="muted-copy">{tool.toolVersionId} · revision {tool.revision}</div></td><td><div className="mono">{tool.providerId}</div><div className="mono">{tool.priceVersionId}</div></td><td><span className={`state-badge state-${tool.state}`}>{stateLabel[tool.state]}</span>{approval && <ApprovalStatus value={approval} />}{policy && <PolicyDecisionStatus value={policy} />}</td><td>{formatTime(tool.updatedAt)}</td><td><div className="credential-actions">{tool.state === 'draft' && <><Button type="button" variant="outline" onClick={() => { setEditingTool(tool.toolVersionId); setToolForm(formFromTool(tool)); }}>编辑</Button><Button type="button" variant="outline" disabled={busy !== ''} onClick={() => void preflightTool(tool.toolVersionId)}>Preflight</Button>{approval?.state === 'approved' ? <Button type="button" disabled={busy !== ''} onClick={() => void act(`publish-tool:${tool.toolVersionId}`, async () => { await gateway.publishToolVersion(workspaceId, tool.toolVersionId); clearPreflight(key); clearPolicyDecision(key); }, 'ToolVersion 已发布，approval 已原子消费。')}>发布已批准 revision</Button> : approval?.state === 'pending' ? <Button type="button" disabled>等待审核</Button> : <Button type="button" disabled={!pf?.ready || busy !== ''} onClick={() => void requestToolReview(tool.toolVersionId)}>提交发布审核</Button>}</>}{tool.state === 'published' && <Button type="button" variant="outline" disabled={busy !== ''} onClick={() => void act(`retire-tool:${tool.toolVersionId}`, async () => { await gateway.retireToolVersion(workspaceId, tool.toolVersionId); }, 'ToolVersion 已退役。')}>退役</Button>}{tool.state === 'retired' && <span className="muted-copy">只读</span>}</div>{pf && <PreflightResult value={pf} />}</td></tr>; })}</tbody></table></div>}
        </section>

        <section className="launch-panel" aria-labelledby="toolset-draft-heading"><div className="panel-heading"><div><p className="section-kicker">Distribution</p><h2 id="toolset-draft-heading">Toolset 草稿与 Binding</h2></div></div>
          <div className="launch-grid"><div className="launch-request"><label>新 Toolset ID<input value={toolsetId} onChange={(event) => setToolsetId(event.target.value)} /></label><Button type="button" disabled={busy !== '' || toolsetId === ''} onClick={() => void act('create-toolset', async () => { await gateway.createToolset(workspaceId, toolsetId); setBindingToolset(toolsetId); setToolsetId(''); }, 'Toolset draft 已创建。')}>创建 Toolset draft</Button></div>
          <div className="launch-request"><label>Draft Toolset<select value={selectedBindingSet} onChange={(event) => setBindingToolset(event.target.value)}><option value="">选择 draft Toolset</option>{draftToolsets.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</select></label><label>ToolVersion<select value={bindingToolVersion} onChange={(event) => setBindingToolVersion(event.target.value)}><option value="">选择 ToolVersion</option>{tools.map((tool) => <option key={tool.toolVersionId} value={tool.toolVersionId}>{tool.toolId}@{tool.version} · {tool.state}</option>)}</select></label><label>Connection<select value={selectedConnection} onChange={(event) => setBindingConnection(event.target.value)}><option value="">选择 Connection</option>{snapshot.data.connections.map((item) => <option key={item.connectionId} value={item.connectionId}>{item.connectionId} · {item.providerId} · {item.state}</option>)}</select></label><label>Budget<select value={selectedBudget} onChange={(event) => setBindingBudget(event.target.value)}><option value="">选择 Budget</option>{snapshot.data.budgetPeriods.map((item) => <option key={`${item.budgetId}:${item.periodId}`} value={item.budgetId}>{item.budgetId} · {item.periodId} · {item.currency}</option>)}</select></label><label><input type="checkbox" checked={bindingMCPExposed} onChange={(event) => setBindingMCPExposed(event.target.checked)} /> MCP exposed</label><label>MCP name<input disabled={!bindingMCPExposed} value={bindingMCPName} onChange={(event) => setBindingMCPName(event.target.value)} /></label><Button type="button" disabled={busy !== '' || !selectedBindingSet || !selectedBindingTool || !selectedConnection || !selectedBudget} onClick={() => void saveBinding()}>保存 Binding draft</Button></div></div>
        </section>

        <section className="run-list-panel" aria-label="Toolset lifecycle"><div className="panel-heading"><div><p className="section-kicker">Toolsets</p><h2>Toolset 生命周期</h2></div></div>
          {toolsets.length === 0 ? <div className="empty-state"><strong>当前 Workspace 没有 Toolset</strong></div> : toolsets.map((set) => { const key = `set:${set.id}`; const pf = preflights[key]; const policy = policyDecisions[key]; const approval = latestApproval(snapshot.data.publicationApprovals, 'toolset', set.id); return <article className="launch-card" key={set.id}><div className="panel-heading"><div><p className="section-kicker">{stateLabel[set.state]}</p><h3 className="mono">{set.id}</h3><p className="muted-copy">revision {set.revision} · Published {formatTime(set.publishedAt)} · Retired {formatTime(set.retiredAt)}</p>{approval && <ApprovalStatus value={approval} />}{policy && <PolicyDecisionStatus value={policy} />}</div><div className="credential-actions">{set.state === 'draft' && <><Button type="button" variant="outline" disabled={busy !== ''} onClick={() => void preflightToolset(set.id)}>Preflight</Button>{approval?.state === 'approved' ? <Button type="button" disabled={busy !== ''} onClick={() => void act(`publish-set:${set.id}`, async () => { await gateway.publishToolset(workspaceId, set.id); clearPreflight(key); clearPolicyDecision(key); }, 'Toolset 已发布，approval 已原子消费。')}>发布已批准 revision</Button> : approval?.state === 'pending' ? <Button type="button" disabled>等待审核</Button> : <Button type="button" disabled={!pf?.ready || busy !== ''} onClick={() => void requestToolsetReview(set.id)}>提交发布审核</Button>}</>}{set.state === 'published' && <Button type="button" variant="outline" disabled={busy !== ''} onClick={() => void act(`retire-set:${set.id}`, async () => { await gateway.retireToolset(workspaceId, set.id); }, 'Toolset 已退役。')}>退役</Button>}</div></div>{pf && <PreflightResult value={pf} />}{set.bindings.length === 0 ? <div className="empty-state"><strong>尚无 Binding</strong></div> : <div className="run-table-wrap"><table className="run-table"><thead><tr><th>Tool</th><th>Connection</th><th>Budget</th><th>MCP</th><th>操作</th></tr></thead><tbody>{set.bindings.map((binding) => <tr key={binding.toolVersionId}><td><span className="mono">{binding.toolId}@{binding.toolVersionLabel}</span><div className="muted-copy">{binding.toolVersionId}</div></td><td className="mono">{binding.connectionId}</td><td className="mono">{binding.budgetId}</td><td>{binding.mcpExposed ? binding.mcpName : '不暴露'}</td><td>{set.state === 'draft' ? <Button type="button" variant="outline" disabled={busy !== ''} onClick={() => void act(`delete-binding:${set.id}:${binding.toolVersionId}`, async () => { await gateway.deleteBinding(workspaceId, set.id, binding.toolVersionId); clearPreflight(key); clearPolicyDecision(key); }, 'Binding draft 已删除。')}>删除</Button> : <span className="muted-copy">immutable</span>}</td></tr>)}</tbody></table></div>}</article>; })}
        </section>
      </>}
    </>}
  </>;
}

function PreflightResult({ value }: { value: CatalogPreflight }) {
  if (value.ready) return <div className="success-panel" role="status"><strong>服务端 Preflight：Ready</strong><span>Publish 仍会在事务中重新校验当前 facts。</span></div>;
  return <div className="error-panel" role="status"><strong>服务端 Preflight：Not ready</strong>{value.issues.map((issue, index) => <span key={`${issue.code}:${issue.targetId}:${index}`}>{issueLabel[issue.code] ?? issue.code} · <span className="mono">{issue.targetId}</span></span>)}</div>;
}
