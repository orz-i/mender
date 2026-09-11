package architecture

import "testing"

func TestSettlementProcessUsesPublicContractsOnlyFromOutboundAdapters(t *testing.T) {
	bad := []struct{ path, source string }{
		{"internal/processes/settlement/application/bad.go", `package application;import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/public"`},
		{"internal/processes/settlement/application/bad.go", `package application;import _ "github.com/jackc/pgx/v5"`},
		{"internal/processes/settlement/adapters/outbound/bad.go", `package outbound;import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/application"`},
		{"internal/processes/settlement/adapters/outbound/bad.go", `package outbound;import _ "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"`},
	}
	for _, c := range bad {
		if len(CheckSource(c.path, []byte(c.source))) == 0 {
			t.Errorf("accepted prohibited settlement dependency: %s", c.source)
		}
	}
	good := `package outbound;import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/public"`
	if v := CheckSource("internal/processes/settlement/adapters/outbound/capabilities/good.go", []byte(good)); len(v) > 0 {
		t.Fatal(v)
	}
}
