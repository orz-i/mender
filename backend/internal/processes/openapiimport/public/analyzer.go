package public

import (
	"context"
	"errors"
)

var (
	ErrInvalidDocument = errors.New("invalid OpenAPI document")
	ErrUnavailable     = errors.New("OpenAPI analyzer unavailable")
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

type Analyzer interface {
	Analyze(context.Context, []byte) (Result, error)
}
