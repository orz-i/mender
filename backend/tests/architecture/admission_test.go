package architecture

import "testing"

func TestAdmissionProcessFollowsTheSameDependencyRule(t *testing.T) {
	bad := []struct{ path, source string }{
		{"internal/processes/admission/application/bad.go", `package application;import _ "github.com/jackc/pgx/v5"`},
		{"internal/processes/admission/application/bad.go", `package application;import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/public"`},
		{"internal/processes/admission/adapters/outbound/bad.go", `package outbound;import _ "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"`},
		{"internal/processes/unknown/application/bad.go", `package application`},
	}
	for _, c := range bad {
		if len(CheckSource(c.path, []byte(c.source))) == 0 {
			t.Errorf("accepted prohibited process dependency: %s", c.source)
		}
	}
	good := `package outbound;import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/public"`
	if v := CheckSource("internal/processes/admission/adapters/outbound/capabilities/good.go", []byte(good)); len(v) > 0 {
		t.Fatal(v)
	}
}
