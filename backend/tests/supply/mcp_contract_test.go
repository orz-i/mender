package supply_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

func reviewedMCPDeployment() domain.Deployment {
	return domain.Deployment{
		Revision: "deploy_mcp", ProviderID: "provider_mcp", TransportKind: "mcp_streamable_http",
		EndpointURL: "https://provider.example/mcp", HTTPMethod: "POST",
		AuthMode: "bearer", MCPProtocolVersion: "2026-07-28", MCPStateless: true,
		RequestTimeout: time.Second, MaxRequestBytes: 64 << 10, MaxResponseBytes: 1 << 20,
		State: "active", CreatedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestReviewedUpstreamMCPDeploymentIsToolsOnlyAndStateless(t *testing.T) {
	base := reviewedMCPDeployment()
	if err := base.Validate(); err != nil || !base.SupportsMCPTools() {
		t.Fatal("reviewed MCP deployment rejected", err)
	}
	for name, mutate := range map[string]func(*domain.Deployment){
		"legacy protocol": func(d *domain.Deployment) { d.MCPProtocolVersion = "2025-11-25" },
		"stateful":        func(d *domain.Deployment) { d.MCPStateless = false },
		"generic retry header": func(d *domain.Deployment) {
			d.IdempotencyHeader = "Idempotency-Key"
		},
		"provider control endpoint": func(d *domain.Deployment) {
			d.StatusEndpointURL, d.StatusHTTPMethod = "https://provider.example/status", "POST"
		},
		"wrong transport": func(d *domain.Deployment) { d.TransportKind = "http" },
	} {
		t.Run(name, func(t *testing.T) {
			d := base
			mutate(&d)
			if d.Validate() == nil {
				t.Fatal("invalid upstream MCP deployment accepted")
			}
		})
	}
}

type snapshotRepo struct {
	items []domain.MCPToolSnapshot
	err   error
}

func (r *snapshotRepo) AppendMCPToolSnapshot(_ context.Context, s domain.MCPToolSnapshot) error {
	if r.err != nil {
		return r.err
	}
	r.items = append(r.items, s)
	return nil
}

func (r *snapshotRepo) ListLatestMCPToolSnapshots(context.Context, string) ([]domain.MCPToolSnapshot, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]domain.MCPToolSnapshot(nil), r.items...), nil
}

func TestMCPToolSnapshotsAreBoundedUntrustedCandidates(t *testing.T) {
	at := time.Date(2026, 9, 11, 12, 30, 0, 0, time.UTC)
	snapshot := domain.MCPToolSnapshot{
		DeploymentRevision: "deploy_mcp", ToolName: "company.search", Title: "Company Search",
		Description: "Untrusted upstream description.", InputSchema: `{"type":"object","properties":{"query":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`, AnnotationsJSON: `{"readOnlyHint":true}`,
		ContentSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", DiscoveredAt: at,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	repo := &snapshotRepo{}
	service, err := application.NewMCPToolCatalog(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Record(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	items, err := service.ListLatest(context.Background(), "deploy_mcp")
	if err != nil || len(items) != 1 || items[0].ToolName != "company.search" {
		t.Fatal(items, err)
	}

	bad := snapshot
	bad.ContentSHA256 = "not-a-digest"
	if err = service.Record(context.Background(), bad); !errors.Is(err, application.ErrMCPToolCatalogUnavailable) {
		t.Fatal("invalid discovery snapshot was not rejected", err)
	}
}
