package document

import "testing"

func TestParseAcceptsInlineJSONAndYAML(t *testing.T) {
	jsonDoc := []byte(`{"openapi":"3.1.0","servers":[{"url":"https://api.example.test"}],"paths":{"/search":{"post":{"operationId":"search","requestBody":{"content":{"application/json":{"schema":{"type":"object"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)
	result, err := Parse(jsonDoc)
	if err != nil || len(result.Operations) != 1 || !result.Operations[0].Importable {
		t.Fatal(result, err)
	}
	yamlDoc := []byte("openapi: 3.1.0\nservers:\n  - url: https://api.example.test\npaths:\n  /search:\n    post:\n      operationId: search\n      requestBody:\n        content:\n          application/json:\n            schema:\n              type: object\n      responses:\n        '200':\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n")
	result, err = Parse(yamlDoc)
	if err != nil || len(result.Operations) != 1 || !result.Operations[0].Importable {
		t.Fatal(result, err)
	}
}

func TestParseRejectsDuplicateJSONKeysMalformedAndOversize(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"openapi":"3.1.0","openapi":"3.0.3"}`),
		[]byte(`{"openapi":`),
		append([]byte("openapi: 3.1.0\n"), make([]byte, maxDocumentBytes)...),
		[]byte{'{', 0, '}'},
	} {
		if _, err := Parse(raw); err == nil {
			t.Fatal("unsafe document accepted")
		}
	}
}
