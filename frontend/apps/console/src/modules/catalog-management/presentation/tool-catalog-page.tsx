import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Badge, Button, Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle, Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle, Input } from '@mender/ui';
import { CatalogLoginRequiredError, type CatalogGateway } from '../application/catalog-gateway';

function errorMessage(error: unknown) {
  if (error instanceof CatalogLoginRequiredError) return 'Your session expired. Sign in again to browse workspace tools.';
  return error instanceof Error ? error.message : 'Tools could not be loaded.';
}

function formatPrice(currency: string, micro: string) {
  try {
    const value = BigInt(micro);
    const whole = value / 1_000_000n;
    const fraction = (value % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '');
    return `${currency} ${whole}${fraction ? `.${fraction}` : ''} max`;
  } catch {
    return `${currency} ${micro}µ`;
  }
}

export function ToolCatalogPage({ gateway }: { gateway: CatalogGateway }) {
  const [params, setParams] = useSearchParams();
  const [query, setQuery] = useState(params.get('q') ?? '');
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const workspaces = useQuery({ queryKey: ['catalog-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const snapshot = useQuery({ queryKey: ['console-catalog', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.snapshot(workspaceId, signal), retry: false });

  const tools = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    return (snapshot.data?.toolVersions ?? [])
      .filter((tool) => tool.state === 'published')
      .filter((tool) => !normalized || `${tool.title} ${tool.description} ${tool.providerId} ${tool.toolId}`.toLocaleLowerCase().includes(normalized));
  }, [query, snapshot.data?.toolVersions]);

  const priceById = useMemo(() => new Map((snapshot.data?.priceVersions ?? []).map((price) => [price.id, price])), [snapshot.data?.priceVersions]);
  const publishedToolsets = useMemo(() => (snapshot.data?.toolsets ?? []).filter((toolset) => toolset.state === 'published'), [snapshot.data?.toolsets]);

  if (workspaces.isError) return <Empty><EmptyHeader><EmptyTitle>Sign in to browse tools</EmptyTitle><EmptyDescription>{errorMessage(workspaces.error)}</EmptyDescription></EmptyHeader><EmptyContent><Button asChild><a href="/auth/login">Sign in</a></Button></EmptyContent></Empty>;

  return <>
    <div className="page-heading-row product-heading">
      <div><h1>Tools</h1><p className="lead">Browse published capabilities available to this workspace. Drafts and publishing controls stay in Publisher.</p></div>
      <Button asChild variant="outline"><Link to="/publisher/catalog">Publisher tools</Link></Button>
    </div>

    <div className="tool-toolbar">
      <Input aria-label="Search tools" value={query} placeholder="Search tools, providers, or capabilities" onChange={(event) => {
        const value = event.target.value;
        setQuery(value);
        const next = new URLSearchParams(params);
        if (value) next.set('q', value); else next.delete('q');
        setParams(next, { replace: true });
      }} />
      {workspaces.data && workspaces.data.length > 1 ? <label className="workspace-select-label">Workspace<select value={workspaceId} onChange={(event) => {
        setSelection(event.target.value);
        const next = new URLSearchParams(params); next.set('workspace', event.target.value); setParams(next, { replace: true });
      }}>{workspaces.data.map((workspace) => <option value={workspace.id} key={workspace.id}>{workspace.id}</option>)}</select></label> : <span className="workspace-context">{workspaceId || 'Workspace'}</span>}
    </div>

    {snapshot.isLoading ? <div className="tool-grid" aria-busy="true">{[0, 1, 2, 3, 4, 5].map((item) => <Card className="tool-card tool-card-loading" key={item}><CardHeader><div className="skeleton-line skeleton-title" /><div className="skeleton-line" /></CardHeader><CardContent><div className="skeleton-line" /></CardContent></Card>)}</div>
      : snapshot.isError ? <Empty><EmptyHeader><EmptyTitle>Tools are unavailable</EmptyTitle><EmptyDescription>{errorMessage(snapshot.error)}</EmptyDescription></EmptyHeader></Empty>
        : tools.length === 0 ? <Empty><EmptyHeader><EmptyTitle>{query ? 'No tools match your search' : 'No published tools yet'}</EmptyTitle><EmptyDescription>{query ? 'Try a different search.' : 'Publish a tool from Publisher, then it will appear here for workspace users.'}</EmptyDescription></EmptyHeader>{!query && <EmptyContent><Button asChild variant="outline"><Link to="/publisher/catalog">Open Publisher tools</Link></Button></EmptyContent>}</Empty>
          : <div className="tool-grid">{tools.map((tool) => {
            const price = priceById.get(tool.priceVersionId);
            const toolsetCount = publishedToolsets.filter((toolset) => toolset.bindings.some((binding) => binding.toolVersionId === tool.toolVersionId)).length;
            return <Card className="tool-card" key={tool.toolVersionId}>
              <CardHeader>
                <div className="tool-card-heading"><div><CardTitle>{tool.title || tool.toolId}</CardTitle><CardDescription>{tool.providerId}</CardDescription></div><Badge variant={tool.sideEffect === 'read_only' ? 'secondary' : 'outline'}>{tool.sideEffect === 'read_only' ? 'Read only' : 'Writes data'}</Badge></div>
              </CardHeader>
              <CardContent className="tool-card-content">
                <p>{tool.description || 'No description provided.'}</p>
                <div className="tool-facts"><span>{price ? formatPrice(price.currency, price.reserveMicro) : 'Pricing unavailable'}</span><span>{toolsetCount > 0 ? `${toolsetCount} toolset${toolsetCount === 1 ? '' : 's'}` : 'Not in a published toolset'}</span>{tool.mcpPublishable && <span>MCP ready</span>}</div>
              </CardContent>
              <CardFooter className="tool-card-footer"><Button asChild disabled={toolsetCount === 0}><Link to="/launch">Run tool</Link></Button><Badge variant="outline">v{tool.version}</Badge></CardFooter>
            </Card>;
          })}</div>}
  </>;
}
