package openapiaccess

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
	openapi "github.com/orz-i/mender/backend/internal/processes/openapiimport/public"
)

type Analyzer struct{ source openapi.Analyzer }

func New(source openapi.Analyzer) *Analyzer { return &Analyzer{source: source} }

func mapDiagnostic(item openapi.Diagnostic) application.OpenAPIDiagnostic {
	return application.OpenAPIDiagnostic{Code: item.Code, Severity: item.Severity, OperationID: item.OperationID, Path: item.Path, Method: item.Method, Message: item.Message}
}

func (a *Analyzer) Analyze(ctx context.Context, raw []byte) (application.OpenAPIPreview, error) {
	if a == nil || a.source == nil {
		return application.OpenAPIPreview{}, application.ErrUnavailable
	}
	result, err := a.source.Analyze(ctx, raw)
	if err != nil {
		if errors.Is(err, openapi.ErrInvalidDocument) {
			return application.OpenAPIPreview{}, application.ErrInvalid
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.OpenAPIPreview{}, err
		}
		return application.OpenAPIPreview{}, application.ErrUnavailable
	}
	preview := application.OpenAPIPreview{OpenAPIVersion: result.OpenAPIVersion, Title: result.Title, Operations: make([]application.OpenAPIOperation, 0, len(result.Operations)), Diagnostics: make([]application.OpenAPIDiagnostic, 0, len(result.Diagnostics))}
	for _, item := range result.Diagnostics {
		preview.Diagnostics = append(preview.Diagnostics, mapDiagnostic(item))
	}
	for _, operation := range result.Operations {
		item := application.OpenAPIOperation{
			OperationID: operation.OperationID, Method: operation.Method, Path: operation.Path, ServerURL: operation.ServerURL,
			Title: operation.Title, Description: operation.Description, InputSchema: operation.InputSchema, OutputSchema: operation.OutputSchema,
			SideEffect: operation.SideEffect, Idempotency: operation.Idempotency, Importable: operation.Importable,
			Diagnostics: make([]application.OpenAPIDiagnostic, 0, len(operation.Diagnostics)),
		}
		for _, diagnostic := range operation.Diagnostics {
			item.Diagnostics = append(item.Diagnostics, mapDiagnostic(diagnostic))
		}
		preview.Operations = append(preview.Operations, item)
	}
	return preview, nil
}

var _ application.OpenAPIAnalyzer = (*Analyzer)(nil)
