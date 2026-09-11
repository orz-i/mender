package supply_test

import (
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

func controlDeployment() domain.Deployment {
	return domain.Deployment{
		Revision: "deploy_control", ProviderID: "provider_control", TransportKind: "http",
		EndpointURL: "https://provider.example/v1/run", HTTPMethod: "POST",
		AuthMode: "none", IdempotencyHeader: "Idempotency-Key", RequestTimeout: time.Second,
		MaxRequestBytes: 4096, MaxResponseBytes: 8192, State: "active", CreatedAt: time.Date(2026, 9, 11, 1, 0, 0, 0, time.UTC),
	}
}

func TestDeploymentControlEndpointsAreOptionalPairedAndPOSTOnly(t *testing.T) {
	base := controlDeployment()
	if err := base.Validate(); err != nil || base.SupportsStatusQuery() || base.SupportsCancellation() {
		t.Fatal("control endpoints should be optional", err)
	}

	withControl := base
	withControl.StatusEndpointURL, withControl.StatusHTTPMethod = "https://provider.example/v1/status", "POST"
	withControl.CancelEndpointURL, withControl.CancelHTTPMethod = "https://provider.example/v1/cancel", "POST"
	if err := withControl.Validate(); err != nil || !withControl.SupportsStatusQuery() || !withControl.SupportsCancellation() {
		t.Fatal("valid provider control endpoints rejected", err)
	}

	for name, mutate := range map[string]func(*domain.Deployment){
		"status endpoint without method": func(d *domain.Deployment) { d.StatusEndpointURL = "https://provider.example/status" },
		"status method without endpoint": func(d *domain.Deployment) { d.StatusHTTPMethod = "POST" },
		"status GET": func(d *domain.Deployment) {
			d.StatusEndpointURL, d.StatusHTTPMethod = "https://provider.example/status", "GET"
		},
		"cancel endpoint without method": func(d *domain.Deployment) { d.CancelEndpointURL = "https://provider.example/cancel" },
		"cancel GET": func(d *domain.Deployment) {
			d.CancelEndpointURL, d.CancelHTTPMethod = "https://provider.example/cancel", "GET"
		},
		"control fragment": func(d *domain.Deployment) {
			d.StatusEndpointURL, d.StatusHTTPMethod = "https://provider.example/status#unsafe", "POST"
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := base
			mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid control endpoint accepted")
			}
		})
	}
}
