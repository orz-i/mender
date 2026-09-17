import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Alert, AlertDescription, AlertTitle, Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Empty, EmptyDescription, EmptyHeader, EmptyTitle, Field, FieldLabel, Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea } from '@mender/ui';
import { ReviewLoginRequiredError, type ReviewGateway } from '../application/review-gateway';
import type { PublicationReview, PublicationReviewState } from '../domain/review';

const stateLabel: Record<PublicationReviewState, string> = { pending: 'Pending', approved: 'Approved', rejected: 'Rejected', consumed: 'Published', expired: 'Expired' };
function message(error: unknown) { return error instanceof Error ? error.message : 'Publication review 操作失败。'; }
function formatTime(value: string | null) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }

export function PublicationReviewPage({ gateway }: { gateway: ReviewGateway }) {
  const queryClient = useQueryClient();
  const [params] = useSearchParams();
  const [selection, setSelection] = useState(params.get('workspace') ?? '');
  const [note, setNote] = useState('');
  const workspaces = useQuery({ queryKey: ['admin-review-workspaces'], queryFn: ({ signal }) => gateway.workspaces(signal), retry: false });
  const workspaceId = selection || workspaces.data?.[0]?.id || '';
  const approvals = useQuery({ queryKey: ['admin-publication-approvals', workspaceId], enabled: workspaceId !== '', queryFn: ({ signal }) => gateway.list(workspaceId, signal), retry: false });
  const approve = useMutation({ mutationFn: (id: string) => gateway.approve(workspaceId, id, note), onSuccess: async () => { setNote(''); await queryClient.invalidateQueries({ queryKey: ['admin-publication-approvals', workspaceId] }); } });
  const reject = useMutation({ mutationFn: (id: string) => gateway.reject(workspaceId, id, note), onSuccess: async () => { setNote(''); await queryClient.invalidateQueries({ queryKey: ['admin-publication-approvals', workspaceId] }); } });
  const loginRequired = workspaces.error instanceof ReviewLoginRequiredError || approvals.error instanceof ReviewLoginRequiredError;

  if (loginRequired) return <Empty><EmptyHeader><EmptyTitle>Sign in to Admin</EmptyTitle><EmptyDescription>Publication reviews require an authenticated reviewer session. Authorization is checked again by the service.</EmptyDescription></EmptyHeader><Button asChild><a href="/auth/login">Sign in</a></Button></Empty>;

  return <>
    <div className="page-heading-row product-heading"><div><h1>Publication reviews</h1><p className="lead">Review exact tool and toolset revisions before they can be published. A requester cannot approve their own change.</p></div><Button type="button" variant="outline" disabled={approvals.isFetching} onClick={() => void approvals.refetch()}>Refresh</Button></div>
    {workspaces.isPending && <Empty><EmptyHeader><EmptyTitle>Loading workspaces…</EmptyTitle></EmptyHeader></Empty>}
    {workspaces.error && !(workspaces.error instanceof ReviewLoginRequiredError) && <Alert variant="destructive"><AlertTitle>Workspaces unavailable</AlertTitle><AlertDescription>{message(workspaces.error)}</AlertDescription></Alert>}
    {workspaces.data && workspaces.data.length > 0 && <Card aria-label="Publication review workspace">
      <CardHeader><CardTitle>Review queue</CardTitle><CardDescription>Approvals are bound to a specific submitted revision. Publishing still runs server preflight.</CardDescription></CardHeader>
      <CardContent className="admin-review-content">
        <div className="admin-review-toolbar"><Field><FieldLabel>Workspace</FieldLabel><Select value={workspaceId} onValueChange={setSelection}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{workspaces.data.map((item) => <SelectItem key={item.id} value={item.id}>{item.id}</SelectItem>)}</SelectGroup></SelectContent></Select></Field><Field className="review-note-field"><FieldLabel htmlFor="decision-note">Decision note</FieldLabel><Textarea id="decision-note" rows={2} maxLength={1000} placeholder="Optional context for this decision" value={note} onChange={(event) => setNote(event.target.value)} /></Field></div>
        {approvals.isPending && <Empty><EmptyHeader><EmptyTitle>Loading review queue…</EmptyTitle></EmptyHeader></Empty>}
        {approvals.error && !(approvals.error instanceof ReviewLoginRequiredError) && <Alert variant="destructive"><AlertTitle>Review queue unavailable</AlertTitle><AlertDescription>{message(approvals.error)}</AlertDescription></Alert>}
        {approve.error && <Alert variant="destructive"><AlertTitle>Approval not completed</AlertTitle><AlertDescription>{message(approve.error)}</AlertDescription></Alert>}
        {reject.error && <Alert variant="destructive"><AlertTitle>Rejection not completed</AlertTitle><AlertDescription>{message(reject.error)}</AlertDescription></Alert>}
        {approvals.data && approvals.data.length === 0 && <Empty><EmptyHeader><EmptyTitle>Nothing to review</EmptyTitle><EmptyDescription>This workspace has no publication requests waiting for a decision.</EmptyDescription></EmptyHeader></Empty>}
        {approvals.data && approvals.data.length > 0 && <ReviewTable workspaceId={workspaceId} items={approvals.data} busy={approve.isPending || reject.isPending} approve={(id) => approve.mutate(id)} reject={(id) => reject.mutate(id)} />}
      </CardContent>
    </Card>}
  </>;
}

function ReviewTable({ workspaceId, items, busy, approve, reject }: { workspaceId: string; items: PublicationReview[]; busy: boolean; approve: (id: string) => void; reject: (id: string) => void }) {
  return <Table><TableHeader><TableRow><TableHead>Change</TableHead><TableHead>Requester</TableHead><TableHead>Status</TableHead><TableHead>Requested</TableHead><TableHead>Decision</TableHead><TableHead className="text-right">Actions</TableHead></TableRow></TableHeader><TableBody>{items.map((item) => <TableRow key={item.id}><TableCell><strong>{item.targetKind === 'tool_version' ? 'Tool version' : 'Toolset'}</strong><div>{item.targetId}</div><div className="muted-copy">revision {item.targetRevision}</div></TableCell><TableCell>{item.requesterUserId}</TableCell><TableCell><Badge variant={item.state === 'pending' ? 'secondary' : item.state === 'rejected' || item.state === 'expired' ? 'outline' : 'default'}>{stateLabel[item.state]}</Badge></TableCell><TableCell><div>{formatTime(item.requestedAt)}</div><div className="muted-copy">Expires {formatTime(item.expiresAt)}</div></TableCell><TableCell><div>{item.reviewerUserId ?? '—'}</div><div className="muted-copy">{item.decisionNote || 'No note'}</div></TableCell><TableCell><div className="credential-actions justify-end">{item.state === 'pending' && <><Button size="sm" type="button" disabled={busy} onClick={() => approve(item.id)}>Approve</Button><Button size="sm" type="button" variant="outline" disabled={busy} onClick={() => reject(item.id)}>Reject</Button></>}<Button asChild size="sm" type="button" variant="ghost"><Link to={`/publication-history?workspace=${encodeURIComponent(workspaceId)}&approval=${encodeURIComponent(item.id)}`}>History</Link></Button></div></TableCell></TableRow>)}</TableBody></Table>;
}
