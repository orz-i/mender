package application

import (
	"context"
	"encoding/json"
	"strings"
)

type OpenAPIDiagnostic struct {
	Code, Severity, OperationID, Path, Method, Message string
}

type OpenAPIOperation struct {
	OperationID, Method, Path, ServerURL, Title, Description string
	InputSchema, OutputSchema                                string
	SideEffect, Idempotency                                  string
	Importable                                               bool
	Diagnostics                                              []OpenAPIDiagnostic
}

type OpenAPIPreview struct {
	OpenAPIVersion string
	Title          string
	Operations     []OpenAPIOperation
	Diagnostics    []OpenAPIDiagnostic
}

type OpenAPIAnalyzer interface {
	Analyze(context.Context, []byte) (OpenAPIPreview, error)
}

type OpenAPIImportInput struct {
	Document                                                   string
	OperationID                                                string
	ToolVersionID, ToolID, Version, ProviderID, PriceVersionID string
	DeploymentRevision                                         string
}

type OpenAPIImportResult struct {
	Tool      ToolVersion
	Operation OpenAPIOperation
}

type OpenAPIImportService struct {
	catalog  *Service
	analyzer OpenAPIAnalyzer
}

func NewOpenAPIImport(catalog *Service, analyzer OpenAPIAnalyzer) (*OpenAPIImportService, error) {
	if catalog == nil || analyzer == nil {
		return nil, ErrUnavailable
	}
	return &OpenAPIImportService{catalog: catalog, analyzer: analyzer}, nil
}

func validOpenAPIOperationID(value string) bool {
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

func canonicalOpenAPIJSONSchema(raw string) (string, bool) {
	if len(raw) < 2 || len(raw) > 1<<20 {
		return "", false
	}
	var schema map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil || schema == nil {
		return "", false
	}
	if offset := decoder.InputOffset(); offset < int64(len(raw)) && strings.TrimSpace(raw[offset:]) != "" {
		return "", false
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func sameOpenAPIJSONSchema(left, right string) bool {
	a, ok := canonicalOpenAPIJSONSchema(left)
	if !ok {
		return false
	}
	b, ok := canonicalOpenAPIJSONSchema(right)
	return ok && a == b
}

func validOpenAPIDiagnostic(item OpenAPIDiagnostic) bool {
	if len(item.Code) < 1 || len(item.Code) > 128 || (item.Severity != "error" && item.Severity != "warning") || len([]rune(item.Message)) < 1 || len([]rune(item.Message)) > 1000 {
		return false
	}
	if item.OperationID != "" && !validOpenAPIOperationID(item.OperationID) {
		return false
	}
	return !strings.ContainsRune(item.Code, 0) && !strings.ContainsRune(item.Message, 0) && !strings.ContainsRune(item.Path, 0) && !strings.ContainsRune(item.Method, 0)
}

func validOpenAPIJSONSchema(raw string, requireObject bool) bool {
	if len(raw) < 2 || len(raw) > 1<<20 {
		return false
	}
	var schema map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil || schema == nil {
		return false
	}
	if offset := decoder.InputOffset(); offset < int64(len(raw)) && strings.TrimSpace(raw[offset:]) != "" {
		return false
	}
	if requireObject {
		kind, ok := schema["type"].(string)
		return ok && kind == "object"
	}
	return true
}

func validOpenAPIPreview(preview OpenAPIPreview) bool {
	if len(preview.OpenAPIVersion) < 5 || len(preview.OpenAPIVersion) > 32 || len(preview.Operations) > 128 || len(preview.Diagnostics) > 512 || len([]rune(preview.Title)) > 200 {
		return false
	}
	for _, item := range preview.Diagnostics {
		if !validOpenAPIDiagnostic(item) {
			return false
		}
	}
	for _, operation := range preview.Operations {
		if !validOpenAPIOperationID(operation.OperationID) || len(operation.Diagnostics) > 64 || len(operation.Path) < 1 || len(operation.Path) > 2048 || len(operation.ServerURL) > 2048 || len([]rune(operation.Title)) > 200 || len([]rune(operation.Description)) > 4000 {
			return false
		}
		for _, item := range operation.Diagnostics {
			if !validOpenAPIDiagnostic(item) || item.OperationID != "" && item.OperationID != operation.OperationID {
				return false
			}
		}
		if operation.Importable {
			if operation.Method != "POST" || !strings.HasPrefix(operation.ServerURL, "https://") || !strings.HasPrefix(operation.Path, "/") || operation.SideEffect != "write" || operation.Idempotency != "unsafe" || !validOpenAPIJSONSchema(operation.InputSchema, true) || !validOpenAPIJSONSchema(operation.OutputSchema, false) {
				return false
			}
		}
	}
	return true
}

func (s *OpenAPIImportService) analyze(ctx context.Context, actor Actor, workspace, document string) (OpenAPIPreview, error) {
	if s == nil || s.catalog == nil || s.analyzer == nil || !validID(actor.UserID) || !validID(workspace) || len(document) < 2 || len(document) > 1<<20 || strings.ContainsRune(document, 0) {
		return OpenAPIPreview{}, ErrInvalid
	}
	if err := s.catalog.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return OpenAPIPreview{}, err
	}
	preview, err := s.analyzer.Analyze(ctx, []byte(document))
	if err != nil {
		return OpenAPIPreview{}, err
	}
	if !validOpenAPIPreview(preview) {
		return OpenAPIPreview{}, ErrUnavailable
	}
	return preview, nil
}

func (s *OpenAPIImportService) Preview(ctx context.Context, actor Actor, workspace, document string) (OpenAPIPreview, error) {
	return s.analyze(ctx, actor, workspace, document)
}

func (s *OpenAPIImportService) Import(ctx context.Context, actor Actor, workspace string, in OpenAPIImportInput) (OpenAPIImportResult, error) {
	if !validOpenAPIOperationID(in.OperationID) || !validID(in.ToolVersionID) || !validID(in.ToolID) || !validVersion(in.Version) || !validID(in.ProviderID) || !validID(in.PriceVersionID) || !validID(in.DeploymentRevision) {
		return OpenAPIImportResult{}, ErrInvalid
	}
	preview, err := s.analyze(ctx, actor, workspace, in.Document)
	if err != nil {
		return OpenAPIImportResult{}, err
	}
	var selected *OpenAPIOperation
	for i := range preview.Operations {
		if preview.Operations[i].OperationID == in.OperationID {
			selected = &preview.Operations[i]
			break
		}
	}
	if selected == nil || !selected.Importable {
		return OpenAPIImportResult{}, ErrInvalid
	}
	tool, err := s.catalog.CreateToolVersion(ctx, actor, workspace, ToolVersionInput{
		ToolVersionID: in.ToolVersionID, ToolID: in.ToolID, Version: in.Version, ProviderID: in.ProviderID,
		PriceVersionID: in.PriceVersionID, DeploymentRevision: in.DeploymentRevision, Title: selected.Title, Description: selected.Description,
		InputSchema: selected.InputSchema, OutputSchema: selected.OutputSchema, SideEffect: selected.SideEffect, Idempotency: selected.Idempotency, MCPPublishable: false,
	})
	if err != nil {
		return OpenAPIImportResult{}, err
	}
	if tool.WorkspaceID != workspace || tool.ToolVersionID != in.ToolVersionID || tool.ToolID != in.ToolID || tool.Version != in.Version || tool.ProviderID != in.ProviderID || tool.PriceVersionID != in.PriceVersionID || tool.DeploymentRevision != in.DeploymentRevision ||
		tool.Title != selected.Title || tool.Description != selected.Description || tool.State != "draft" || tool.MCPPublishable || !sameOpenAPIJSONSchema(tool.InputSchema, selected.InputSchema) || !sameOpenAPIJSONSchema(tool.OutputSchema, selected.OutputSchema) || tool.SideEffect != selected.SideEffect || tool.Idempotency != selected.Idempotency {
		return OpenAPIImportResult{}, ErrUnavailable
	}
	return OpenAPIImportResult{Tool: tool, Operation: *selected}, nil
}
