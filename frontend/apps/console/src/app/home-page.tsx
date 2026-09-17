import { Link } from 'react-router';
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Separator } from '@mender/ui';

export function HomePage() {
  return <>
    <div className="page-heading-row product-heading">
      <div>
        <h1>Welcome to Mender</h1>
        <p className="lead">Discover tools, connect your accounts, and run capabilities from one workspace.</p>
      </div>
      <Badge variant="secondary">Local demo</Badge>
    </div>

    <section className="onboarding-stack" aria-label="Get started">
      <Card className="onboarding-card">
        <CardHeader>
          <div className="step-heading"><span className="step-number">1</span><div><CardTitle>Choose a tool</CardTitle><CardDescription>Browse the tools your workspace can use. Pricing, access and side effects are shown before a run starts.</CardDescription></div></div>
        </CardHeader>
        <CardContent className="onboarding-actions"><Button asChild><Link to="/catalog">Browse tools</Link></Button></CardContent>
      </Card>

      <Card className="onboarding-card">
        <CardHeader>
          <div className="step-heading"><span className="step-number">2</span><div><CardTitle>Connect an account</CardTitle><CardDescription>Authorize the provider account a tool needs. Mender keeps provider credentials out of the browser.</CardDescription></div></div>
        </CardHeader>
        <CardContent className="onboarding-actions"><Button asChild variant="outline"><Link to="/connections">Manage connections</Link></Button></CardContent>
      </Card>

      <Card className="onboarding-card">
        <CardHeader>
          <div className="step-heading"><span className="step-number">3</span><div><CardTitle>Run and observe</CardTitle><CardDescription>Launch an authorized tool, then inspect status, output and usage without exposing internal execution details.</CardDescription></div></div>
        </CardHeader>
        <CardContent className="onboarding-actions"><Button asChild variant="outline"><Link to="/launch">Run a tool</Link></Button><Button asChild variant="ghost"><Link to="/runs">View runs</Link></Button></CardContent>
      </Card>
    </section>

    <Separator className="my-8" />
    <section className="quick-links" aria-label="Workspace overview">
      <Link to="/catalog"><strong>Tools</strong><span>Browse workspace capabilities</span></Link>
      <Link to="/usage"><strong>Usage</strong><span>Review budgets and run charges</span></Link>
      <Link to="/publisher"><strong>Publisher</strong><span>Publish your own integrations</span></Link>
    </section>
  </>;
}
