package application

import (
	"encoding/json"
	"testing"
)

func parseRoot(t *testing.T, raw string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	return root
}

func hasDiagnostic(items []Diagnostic, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func TestAnalyzeAcceptsReviewedPostJSONAndDereferencesLocalSchemas(t *testing.T) {
	root := parseRoot(t, `{
      "openapi":"3.1.0","info":{"title":"Search API"},"servers":[{"url":"https://api.example.test/v1"}],
      "components":{"schemas":{"SearchRequest":{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}},"required":["query"]},"SearchResponse":{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}}}},"securitySchemes":{"bearer":{"type":"http","scheme":"bearer"}}},
      "security":[{"bearer":[]}],
      "paths":{"/search":{"post":{"operationId":"searchCompanies","summary":"Search companies","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/SearchRequest"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/SearchResponse"}}}}}}}}
    }`)
	result := Analyze(root)
	if len(result.Diagnostics) != 0 || result.OpenAPIVersion != "3.1.0" || len(result.Operations) != 1 {
		t.Fatal(result)
	}
	op := result.Operations[0]
	if !op.Importable || op.OperationID != "searchCompanies" || op.Method != "POST" || op.Path != "/search" || op.ServerURL != "https://api.example.test/v1" || op.SideEffect != "write" || op.Idempotency != "unsafe" {
		t.Fatal(op)
	}
	if !json.Valid([]byte(op.InputSchema)) || !json.Valid([]byte(op.OutputSchema)) || !hasDiagnostic(op.Diagnostics, "security_runtime_owned") {
		t.Fatal(op)
	}
	if containsRef(op.InputSchema) || containsRef(op.OutputSchema) {
		t.Fatal("resolved schemas retained refs", op)
	}
}

func containsRef(raw string) bool {
	return len(raw) > 0 && (stringContains(raw, `"$ref"`) || stringContains(raw, "#/components/"))
}

func stringContains(raw, wanted string) bool {
	for i := 0; i+len(wanted) <= len(raw); i++ {
		if raw[i:i+len(wanted)] == wanted {
			return true
		}
	}
	return false
}

func TestAnalyzeFailsClosedForExecutorUnsupportedOperations(t *testing.T) {
	root := parseRoot(t, `{
      "openapi":"3.0.3","info":{"title":"Mixed"},"servers":[{"url":"https://api.example.test"}],
      "paths":{
        "/items/{id}":{"get":{"operationId":"getItem","parameters":[{"in":"path","name":"id","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}},
        "/upload":{"post":{"operationId":"upload","requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}
      }
    }`)
	result := Analyze(root)
	if len(result.Operations) != 2 {
		t.Fatal(result)
	}
	if result.Operations[0].Importable || !hasDiagnostic(result.Operations[0].Diagnostics, "http_method_unsupported") || !hasDiagnostic(result.Operations[0].Diagnostics, "path_parameters_unsupported") {
		t.Fatal(result.Operations[0])
	}
	if result.Operations[1].Importable || !hasDiagnostic(result.Operations[1].Diagnostics, "application_json_request_required") {
		t.Fatal(result.Operations[1])
	}
}

func TestAnalyzeRejectsRemoteRefsCyclesAndAmbiguousSuccessResponses(t *testing.T) {
	remote := parseRoot(t, `{"openapi":"3.1.0","servers":[{"url":"https://api.example.test"}],"paths":{"/x":{"post":{"operationId":"remoteRef","requestBody":{"content":{"application/json":{"schema":{"$ref":"https://evil.example/schema.json"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)
	remoteResult := Analyze(remote)
	if !hasDiagnostic(remoteResult.Diagnostics, "external_ref_forbidden") || remoteResult.Operations[0].Importable {
		t.Fatal(remoteResult)
	}

	cycle := parseRoot(t, `{"openapi":"3.1.0","servers":[{"url":"https://api.example.test"}],"components":{"schemas":{"A":{"$ref":"#/components/schemas/B"},"B":{"$ref":"#/components/schemas/A"}}},"paths":{"/x":{"post":{"operationId":"cycle","requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/A"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}},"201":{"description":"also ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)
	cycleResult := Analyze(cycle)
	if cycleResult.Operations[0].Importable || !hasDiagnostic(cycleResult.Operations[0].Diagnostics, "schema_ref_cycle") || !hasDiagnostic(cycleResult.Operations[0].Diagnostics, "single_success_response_required") {
		t.Fatal(cycleResult.Operations[0])
	}
}

func TestAnalyzeRejectsSwaggerVariableServerAndOperationIDCollision(t *testing.T) {
	if result := Analyze(parseRoot(t, `{"swagger":"2.0","paths":{}}`)); !hasDiagnostic(result.Diagnostics, "unsupported_openapi_version") {
		t.Fatal(result)
	}
	root := parseRoot(t, `{"openapi":"3.0.3","servers":[{"url":"https://{tenant}.example.test"}],"paths":{"/a":{"post":{"operationId":"same","requestBody":{"content":{"application/json":{"schema":{"type":"object"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}},"/b":{"post":{"operationId":"same","requestBody":{"content":{"application/json":{"schema":{"type":"object"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)
	result := Analyze(root)
	if !hasDiagnostic(result.Diagnostics, "https_server_required") || result.Operations[0].Importable || result.Operations[1].Importable || !hasDiagnostic(result.Operations[0].Diagnostics, "operation_id_duplicate") || !hasDiagnostic(result.Operations[1].Diagnostics, "operation_id_duplicate") {
		t.Fatal(result)
	}
}
