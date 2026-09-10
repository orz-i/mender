package architecture

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestRepositoryArchitecture(t *testing.T) {
	violations, err := CheckTree(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v.String())
	}
}

func TestBoundaryFixtures(t *testing.T) {
	for _, tc := range []struct{ name, file, source, rule string }{
		{"framework in domain", "internal/contexts/execution/domain/run.go", `package domain; import _ "github.com/gin-gonic/gin"`, "GO_PURE_IMPORT"},
		{"SQL in application", "internal/contexts/execution/application/run.go", `package application; import _ "database/sql"`, "GO_PURE_IMPORT"},
		{"reverse adapter dependency", "internal/contexts/execution/application/run.go", `package application; import _ "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"`, "GO_BOUNDARY"},
		{"cross context model", "internal/contexts/execution/domain/run.go", `package domain; import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/domain"`, "GO_BOUNDARY"},
		{"domain via application", "internal/contexts/execution/domain/run.go", `package domain; import _ "github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"`, "GO_BOUNDARY"},
		{"public leaks model", "internal/contexts/execution/public/run.go", `package public; import _ "github.com/orz-i/mender/backend/internal/contexts/execution/domain"`, "GO_BOUNDARY"},
		{"inbound bypass", "internal/contexts/execution/adapters/inbound/httpapi/run.go", `package httpapi; import _ "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"`, "GO_BOUNDARY"},
		{"shared kernel escape", "internal/sharedkernel/ids.go", `package sharedkernel; import _ "net/http"`, "GO_PURE_IMPORT"},
		{"platform business dependency", "internal/platform/config.go", `package platform; import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/domain"`, "GO_PLATFORM"},
		{"clock alias", "internal/contexts/execution/domain/run.go", `package domain; import clock "time"; var now = clock.Now`, "GO_AMBIENT_TIME"},
		{"dot import", "internal/contexts/execution/domain/run.go", `package domain; import . "time"`, "GO_DOT_IMPORT"},
		{"console effect", "internal/contexts/execution/domain/run.go", `package domain; import "fmt"; var output = fmt.Println`, "GO_AMBIENT_IO"},
		{"unknown context", "internal/contexts/unreviewed/domain/run.go", `package domain`, "GO_CONTEXT"},
		{"unknown layer", "internal/contexts/execution/helpers/util.go", `package helpers`, "GO_LAYER"},
		{"inactive tag still inspected", "internal/contexts/execution/domain/run_linux.go", "//go:build linux\n\npackage domain\nimport _ \"net/http\"", "GO_PURE_IMPORT"},
		{"test not blanket ignored", "internal/contexts/execution/domain/run_test.go", `package domain; import _ "database/sql"`, "GO_PURE_IMPORT"},
		{"invalid source", "internal/contexts/execution/domain/run.go", `package`, "GO_PARSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckSource(tc.file, []byte(tc.source))
			for _, v := range got {
				if v.Rule == tc.rule {
					return
				}
			}
			t.Fatalf("expected %s, got %v", tc.rule, got)
		})
	}
}

func TestAllowedBoundaries(t *testing.T) {
	for _, tc := range []struct{ file, source string }{
		{"internal/contexts/execution/domain/run.go", `package domain; import "time"; type ClockValue struct { At time.Time }`},
		{"internal/contexts/execution/application/ports/run.go", `package ports; import _ "github.com/orz-i/mender/backend/internal/contexts/execution/domain"`},
		{"internal/contexts/execution/adapters/outbound/commerce/run.go", `package commerce; import _ "github.com/orz-i/mender/backend/internal/contexts/commerce/public"`},
		{"internal/contexts/execution/adapters/inbound/httpapi/run.go", `package httpapi; import _ "github.com/gin-gonic/gin"`},
		{"internal/bootstrap/run.go", `package bootstrap; import _ "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"`},
		{"internal/contexts/execution/domain/run_test.go", `package domain; import _ "testing"`},
	} {
		if got := CheckSource(tc.file, []byte(tc.source)); len(got) != 0 {
			t.Errorf("%s: %v", tc.file, got)
		}
	}
}

func TestScanCannotSilentlyPassEmptyTree(t *testing.T) {
	if _, err := CheckTree(fstest.MapFS{"internal/.keep": &fstest.MapFile{}}); err == nil {
		t.Fatal("empty source tree must fail")
	}
}
