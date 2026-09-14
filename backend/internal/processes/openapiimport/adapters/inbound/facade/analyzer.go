package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/processes/openapiimport/adapters/inbound/document"
	"github.com/orz-i/mender/backend/internal/processes/openapiimport/application"
	"github.com/orz-i/mender/backend/internal/processes/openapiimport/public"
)

type Analyzer struct{}

func New() *Analyzer { return &Analyzer{} }

func mapDiagnostic(item application.Diagnostic) public.Diagnostic {
	return public.Diagnostic{Code: item.Code, Severity: item.Severity, OperationID: item.OperationID, Path: item.Path, Method: item.Method, Message: item.Message}
}

func mapResult(result application.Result) public.Result {
	mapped := public.Result{OpenAPIVersion: result.OpenAPIVersion, Title: result.Title, Operations: make([]public.Operation, 0, len(result.Operations)), Diagnostics: make([]public.Diagnostic, 0, len(result.Diagnostics))}
	for _, item := range result.Diagnostics {
		mapped.Diagnostics = append(mapped.Diagnostics, mapDiagnostic(item))
	}
	for _, operation := range result.Operations {
		item := public.Operation{
			OperationID: operation.OperationID, Method: operation.Method, Path: operation.Path, ServerURL: operation.ServerURL,
			Title: operation.Title, Description: operation.Description, InputSchema: operation.InputSchema, OutputSchema: operation.OutputSchema,
			SideEffect: operation.SideEffect, Idempotency: operation.Idempotency, Importable: operation.Importable,
			Diagnostics: make([]public.Diagnostic, 0, len(operation.Diagnostics)),
		}
		for _, diagnostic := range operation.Diagnostics {
			item.Diagnostics = append(item.Diagnostics, mapDiagnostic(diagnostic))
		}
		mapped.Operations = append(mapped.Operations, item)
	}
	return mapped
}

func (a *Analyzer) Analyze(ctx context.Context, raw []byte) (public.Result, error) {
	if err := ctx.Err(); err != nil {
		return public.Result{}, err
	}
	result, err := document.Parse(raw)
	if err != nil {
		if errors.Is(err, document.ErrInvalidDocument) {
			return public.Result{}, public.ErrInvalidDocument
		}
		return public.Result{}, public.ErrUnavailable
	}
	return mapResult(result), nil
}

var _ public.Analyzer = (*Analyzer)(nil)
