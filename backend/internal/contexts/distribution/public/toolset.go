package public

import (
	"context"
	"errors"
)

var (
	ErrNotFound    = errors.New("toolset binding unavailable")
	ErrUnavailable = errors.New("distribution unavailable")
)

type Binding struct{ ToolsetVersionID, ToolVersionID, BudgetID string }

type Toolsets interface {
	ResolveBinding(context.Context, string, string, string, string) (Binding, error)
}
