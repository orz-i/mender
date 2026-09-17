import { Link } from 'react-router';
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle } from '@mender/ui';

export function HomePage() {
  return <>
    <div className="page-heading-row product-heading"><div><h1>Admin overview</h1><p className="lead">Review publishing changes, respond to incidents, and manage workspace-wide governance from one place.</p></div><Badge variant="secondary">Reviewer</Badge></div>
    <section className="admin-overview-grid" aria-label="Admin tasks">
      <Card>
        <CardHeader><CardTitle>Publication reviews</CardTitle><CardDescription>Approve or reject exact submitted revisions. Self-review remains blocked by the service.</CardDescription></CardHeader>
        <CardContent><Button asChild><Link to="/publication-reviews">Open review queue</Link></Button></CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Release operations</CardTitle><CardDescription>Inspect reviewed releases, roll back a revision, or stop new work during an incident.</CardDescription></CardHeader>
        <CardContent><Button asChild variant="outline"><Link to="/release-management">Manage releases</Link></Button></CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Execution governance</CardTitle><CardDescription>Manage server-enforced policy revisions and inspect decisions without exposing run arguments.</CardDescription></CardHeader>
        <CardContent><Button asChild variant="outline"><Link to="/execution-governance">Open execution policy</Link></Button></CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Billing & payments</CardTitle><CardDescription>Review immutable billing facts and sandbox payment state. Real-money funding stays disabled.</CardDescription></CardHeader>
        <CardContent><Button asChild variant="outline"><Link to="/billing">Open billing</Link></Button></CardContent>
      </Card>
    </section>
    <section className="admin-secondary-actions" aria-label="Other admin areas">
      <Link to="/platform-operations"><strong>Incidents</strong><span>Workspace freezes and platform events</span></Link>
      <Link to="/support-approvals"><strong>Approvals & support</strong><span>JIT access and dangerous operations</span></Link>
      <Link to="/publication-history"><strong>Audit history</strong><span>Append-only governance timeline</span></Link>
      <Link to="/publication-policy"><strong>Publication policy</strong><span>Review declarative publication rules</span></Link>
    </section>
  </>;
}
