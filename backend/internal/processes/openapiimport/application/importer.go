package application

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	maxOperations  = 128
	maxSchemaDepth = 32
	maxSchemaNodes = 4096
)

type Diagnostic struct {
	Code, Severity, OperationID, Path, Method, Message string
}

type Operation struct {
	OperationID, Method, Path, ServerURL, Title, Description string
	InputSchema, OutputSchema                                string
	SideEffect, Idempotency                                  string
	Importable                                               bool
	Diagnostics                                              []Diagnostic
}

type Result struct {
	OpenAPIVersion string
	Title          string
	Operations     []Operation
	Diagnostics    []Diagnostic
}

func diagnostic(code, severity, operationID, path, method, message string) Diagnostic {
	return Diagnostic{Code: code, Severity: severity, OperationID: operationID, Path: path, Method: method, Message: message}
}

func blocking(items []Diagnostic) bool {
	for _, item := range items {
		if item.Severity == "error" {
			return true
		}
	}
	return false
}

func object(value any) (map[string]any, bool) {
	v, ok := value.(map[string]any)
	return v, ok && v != nil
}

func array(value any) ([]any, bool) {
	v, ok := value.([]any)
	return v, ok
}

func text(value any) (string, bool) {
	v, ok := value.(string)
	return v, ok && v != ""
}

func validVersion(value string) bool {
	if strings.HasPrefix(value, "3.0.") || strings.HasPrefix(value, "3.1.") {
		rest := value[4:]
		if rest == "" {
			return false
		}
		for _, ch := range rest {
			if ch < '0' || ch > '9' {
				return false
			}
		}
		return true
	}
	return false
}

func validOperationID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.') {
			return false
		}
	}
	return true
}

func validHTTPSURL(value string) bool {
	if len(value) < len("https://a") || len(value) > 2048 || !strings.HasPrefix(value, "https://") || strings.ContainsAny(value, "{}\x00\r\n\t ") {
		return false
	}
	rest := strings.TrimPrefix(value, "https://")
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	host := rest[:end]
	return host != "" && !strings.Contains(host, "@") && !strings.Contains(value, "#") && !strings.Contains(value, "?")
}

func rootServer(root map[string]any) (string, Diagnostic) {
	servers, ok := array(root["servers"])
	if !ok || len(servers) != 1 {
		return "", diagnostic("single_https_server_required", "error", "", "", "", "OpenAPI Importer requires exactly one reviewed HTTPS server URL.")
	}
	server, ok := object(servers[0])
	if !ok || len(server) != 1 {
		return "", diagnostic("server_object_unsupported", "error", "", "", "", "Server variables and extra server metadata are not imported in this Alpha.")
	}
	url, ok := text(server["url"])
	if !ok || !validHTTPSURL(url) {
		return "", diagnostic("https_server_required", "error", "", "", "", "Server URL must be a fixed HTTPS URL without variables, query or fragment.")
	}
	return strings.TrimSuffix(url, "/"), Diagnostic{}
}

func scanRefs(value any, path string, out *[]Diagnostic) {
	switch current := value.(type) {
	case map[string]any:
		if raw, exists := current["$ref"]; exists {
			ref, ok := raw.(string)
			if !ok || !strings.HasPrefix(ref, "#/components/schemas/") {
				code := "external_ref_forbidden"
				if ok && strings.HasPrefix(ref, "#/") {
					code = "ref_scope_unsupported"
				}
				*out = append(*out, diagnostic(code, "error", "", path, "", "Only local #/components/schemas references are accepted; remote loading is disabled."))
			}
		}
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			scanRefs(current[key], path+"/"+key, out)
		}
	case []any:
		for i, item := range current {
			scanRefs(item, path+"/"+jsonIndex(i), out)
		}
	}
}

func jsonIndex(value int) string {
	if value == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for value > 0 {
		pos--
		buf[pos] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[pos:])
}

func pointerName(ref string) (string, bool) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(ref, prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	name = strings.ReplaceAll(strings.ReplaceAll(name, "~1", "/"), "~0", "~")
	return name, name != ""
}

type schemaResolver struct {
	schemas map[string]any
	nodes   int
}

func (r *schemaResolver) resolve(value any, stack map[string]bool, depth int) (any, string) {
	if depth > maxSchemaDepth {
		return nil, "schema_depth_exceeded"
	}
	r.nodes++
	if r.nodes > maxSchemaNodes {
		return nil, "schema_size_exceeded"
	}
	switch current := value.(type) {
	case map[string]any:
		if raw, exists := current["$ref"]; exists {
			if len(current) != 1 {
				return nil, "ref_siblings_unsupported"
			}
			ref, ok := raw.(string)
			if !ok {
				return nil, "ref_invalid"
			}
			name, ok := pointerName(ref)
			if !ok {
				return nil, "ref_scope_unsupported"
			}
			if stack[ref] {
				return nil, "schema_ref_cycle"
			}
			target, ok := r.schemas[name]
			if !ok {
				return nil, "schema_ref_not_found"
			}
			stack[ref] = true
			resolved, code := r.resolve(target, stack, depth+1)
			delete(stack, ref)
			return resolved, code
		}
		result := make(map[string]any, len(current))
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			resolved, code := r.resolve(current[key], stack, depth+1)
			if code != "" {
				return nil, code
			}
			result[key] = resolved
		}
		return result, ""
	case []any:
		result := make([]any, len(current))
		for i, item := range current {
			resolved, code := r.resolve(item, stack, depth+1)
			if code != "" {
				return nil, code
			}
			result[i] = resolved
		}
		return result, ""
	default:
		return current, ""
	}
}

func componentsSchemas(root map[string]any) map[string]any {
	components, ok := object(root["components"])
	if !ok {
		return map[string]any{}
	}
	schemas, ok := object(components["schemas"])
	if !ok {
		return map[string]any{}
	}
	return schemas
}

func schemaJSON(root map[string]any, schema any, requireObject bool) (string, string) {
	resolver := schemaResolver{schemas: componentsSchemas(root)}
	resolved, code := resolver.resolve(schema, map[string]bool{}, 0)
	if code != "" {
		return "", code
	}
	obj, ok := object(resolved)
	if !ok {
		return "", "schema_object_required"
	}
	if requireObject {
		kind, ok := obj["type"].(string)
		if !ok || kind != "object" {
			return "", "request_object_schema_required"
		}
	}
	encoded, err := json.Marshal(obj)
	if err != nil || len(encoded) > 1<<20 {
		return "", "schema_encoding_failed"
	}
	return string(encoded), ""
}

func requestSchema(root, operation map[string]any) (string, string) {
	body, ok := object(operation["requestBody"])
	if !ok || len(body) == 0 {
		return "", "json_request_body_required"
	}
	if _, exists := body["$ref"]; exists {
		return "", "request_body_ref_unsupported"
	}
	content, ok := object(body["content"])
	if !ok || len(content) != 1 {
		return "", "single_json_request_content_required"
	}
	media, ok := object(content["application/json"])
	if !ok || len(media) == 0 {
		return "", "application_json_request_required"
	}
	schema, exists := media["schema"]
	if !exists {
		return "", "request_schema_required"
	}
	return schemaJSON(root, schema, true)
}

func successResponseSchema(root, operation map[string]any) (string, string) {
	responses, ok := object(operation["responses"])
	if !ok || len(responses) == 0 {
		return "", "responses_required"
	}
	keys := make([]string, 0, len(responses))
	for key := range responses {
		if len(key) == 3 && key[0] == '2' && key[1] >= '0' && key[1] <= '9' && key[2] >= '0' && key[2] <= '9' {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) != 1 {
		return "", "single_success_response_required"
	}
	response, ok := object(responses[keys[0]])
	if !ok || len(response) == 0 {
		return "", "success_response_object_required"
	}
	if _, exists := response["$ref"]; exists {
		return "", "response_ref_unsupported"
	}
	content, ok := object(response["content"])
	if !ok || len(content) != 1 {
		return "", "single_json_response_content_required"
	}
	media, ok := object(content["application/json"])
	if !ok || len(media) == 0 {
		return "", "application_json_response_required"
	}
	schema, exists := media["schema"]
	if !exists {
		return "", "response_schema_required"
	}
	return schemaJSON(root, schema, false)
}

func operationSecurity(root, operation map[string]any, operationID, path string) []Diagnostic {
	raw, exists := operation["security"]
	if !exists {
		raw = root["security"]
	}
	if raw == nil {
		return nil
	}
	requirements, ok := array(raw)
	if !ok {
		return []Diagnostic{diagnostic("security_invalid", "error", operationID, path, "POST", "Security requirements must be an array.")}
	}
	if len(requirements) == 0 {
		return nil
	}
	components, _ := object(root["components"])
	schemes, _ := object(components["securitySchemes"])
	for _, requirement := range requirements {
		entry, ok := object(requirement)
		if !ok || len(entry) == 0 {
			return []Diagnostic{diagnostic("security_invalid", "error", operationID, path, "POST", "Security requirement entries must reference declared schemes.")}
		}
		for name := range entry {
			if _, exists := schemes[name]; !exists {
				return []Diagnostic{diagnostic("security_scheme_not_found", "error", operationID, path, "POST", "A referenced security scheme is not declared.")}
			}
		}
	}
	return []Diagnostic{diagnostic("security_runtime_owned", "warning", operationID, path, "POST", "OpenAPI security is advisory only; runtime credentials remain owned by reviewed Mender Connection/Deployment configuration.")}
}

func analyzeOperation(root map[string]any, serverURL, path, method string, raw any) Operation {
	op := Operation{Method: strings.ToUpper(method), Path: path, ServerURL: serverURL, SideEffect: "write", Idempotency: "unsafe"}
	operation, ok := object(raw)
	if !ok {
		op.Diagnostics = append(op.Diagnostics, diagnostic("operation_object_required", "error", "", path, op.Method, "Operation must be an object."))
		return op
	}
	if id, ok := text(operation["operationId"]); ok {
		op.OperationID = id
	}
	if !validOperationID(op.OperationID) {
		op.Diagnostics = append(op.Diagnostics, diagnostic("operation_id_required", "error", op.OperationID, path, op.Method, "A unique ASCII operationId is required."))
	}
	if summary, ok := operation["summary"].(string); ok {
		op.Title = summary
	}
	if op.Title == "" {
		op.Title = op.OperationID
	}
	if description, ok := operation["description"].(string); ok {
		op.Description = description
	}
	if method != "post" {
		op.Diagnostics = append(op.Diagnostics, diagnostic("http_method_unsupported", "error", op.OperationID, path, op.Method, "Current reviewed HTTP Executor accepts POST JSON body operations only."))
	}
	if strings.Contains(path, "{") || strings.Contains(path, "}") {
		op.Diagnostics = append(op.Diagnostics, diagnostic("path_parameters_unsupported", "error", op.OperationID, path, op.Method, "Path templating is not mapped by the current HTTP Executor."))
	}
	if params, exists := operation["parameters"]; exists {
		if values, ok := array(params); !ok || len(values) > 0 {
			op.Diagnostics = append(op.Diagnostics, diagnostic("operation_parameters_unsupported", "error", op.OperationID, path, op.Method, "Path/query/header/cookie parameters are not mapped by the current HTTP Executor."))
		}
	}
	if _, exists := operation["callbacks"]; exists {
		op.Diagnostics = append(op.Diagnostics, diagnostic("callbacks_unsupported", "error", op.OperationID, path, op.Method, "OpenAPI callbacks are not imported as provider callbacks."))
	}
	if servers, exists := operation["servers"]; exists {
		if values, ok := array(servers); !ok || len(values) > 0 {
			op.Diagnostics = append(op.Diagnostics, diagnostic("operation_servers_unsupported", "error", op.OperationID, path, op.Method, "Operation-level server overrides are not imported."))
		}
	}
	if deprecated, ok := operation["deprecated"].(bool); ok && deprecated {
		op.Diagnostics = append(op.Diagnostics, diagnostic("operation_deprecated", "error", op.OperationID, path, op.Method, "Deprecated operations are not imported automatically."))
	}
	op.Diagnostics = append(op.Diagnostics, operationSecurity(root, operation, op.OperationID, path)...)
	if method == "post" {
		if input, code := requestSchema(root, operation); code != "" {
			op.Diagnostics = append(op.Diagnostics, diagnostic(code, "error", op.OperationID, path, op.Method, "Request body is outside the supported POST application/json schema subset."))
		} else {
			op.InputSchema = input
		}
		if output, code := successResponseSchema(root, operation); code != "" {
			op.Diagnostics = append(op.Diagnostics, diagnostic(code, "error", op.OperationID, path, op.Method, "Successful response is outside the supported single application/json schema subset."))
		} else {
			op.OutputSchema = output
		}
	}
	op.Importable = !blocking(op.Diagnostics) && op.OperationID != "" && op.InputSchema != "" && op.OutputSchema != ""
	return op
}

func Analyze(root map[string]any) Result {
	result := Result{}
	version, ok := text(root["openapi"])
	if !ok || !validVersion(version) {
		result.Diagnostics = append(result.Diagnostics, diagnostic("unsupported_openapi_version", "error", "", "", "", "Only OpenAPI 3.0.x and 3.1.x are accepted."))
		return result
	}
	result.OpenAPIVersion = version
	if info, ok := object(root["info"]); ok {
		result.Title, _ = info["title"].(string)
	}
	server, serverDiagnostic := rootServer(root)
	if serverDiagnostic.Code != "" {
		result.Diagnostics = append(result.Diagnostics, serverDiagnostic)
	}
	if raw, exists := root["webhooks"]; exists {
		if hooks, ok := object(raw); !ok || len(hooks) > 0 {
			result.Diagnostics = append(result.Diagnostics, diagnostic("webhooks_unsupported", "error", "", "", "", "OpenAPI webhooks are outside this importer; Mender provider callbacks use a separate reviewed contract."))
		}
	}
	scanRefs(root, "", &result.Diagnostics)
	paths, ok := object(root["paths"])
	if !ok || len(paths) == 0 {
		result.Diagnostics = append(result.Diagnostics, diagnostic("paths_required", "error", "", "", "", "OpenAPI document must contain at least one path."))
		return result
	}
	pathNames := make([]string, 0, len(paths))
	for path := range paths {
		pathNames = append(pathNames, path)
	}
	sort.Strings(pathNames)
	seen := map[string]bool{}
	methods := []string{"delete", "get", "head", "options", "patch", "post", "put", "trace"}
	for _, path := range pathNames {
		if len(result.Operations) >= maxOperations {
			result.Diagnostics = append(result.Diagnostics, diagnostic("operation_limit_exceeded", "error", "", path, "", "OpenAPI document exceeds the 128 operation import limit."))
			break
		}
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\x00\r\n") {
			result.Diagnostics = append(result.Diagnostics, diagnostic("path_invalid", "error", "", path, "", "OpenAPI path must be an absolute relative path beginning with '/'."))
			continue
		}
		pathItem, ok := object(paths[path])
		if !ok {
			result.Diagnostics = append(result.Diagnostics, diagnostic("path_item_invalid", "error", "", path, "", "OpenAPI path item must be an object."))
			continue
		}
		if params, exists := pathItem["parameters"]; exists {
			if values, ok := array(params); !ok || len(values) > 0 {
				result.Diagnostics = append(result.Diagnostics, diagnostic("path_parameters_unsupported", "error", "", path, "", "Path-level parameters are not mapped by the current HTTP Executor."))
			}
		}
		for _, method := range methods {
			raw, exists := pathItem[method]
			if !exists {
				continue
			}
			op := analyzeOperation(root, server, path, method, raw)
			if seen[op.OperationID] && op.OperationID != "" {
				op.Diagnostics = append(op.Diagnostics, diagnostic("operation_id_duplicate", "error", op.OperationID, path, strings.ToUpper(method), "operationId must be unique within the document."))
				op.Importable = false
			}
			if op.OperationID != "" {
				seen[op.OperationID] = true
			}
			if server == "" || blocking(result.Diagnostics) {
				op.Importable = false
			}
			result.Operations = append(result.Operations, op)
		}
	}
	return result
}
