package public

import (
	"context"
	"errors"
)

var (
	ErrNotFound    = errors.New("toolset binding unavailable")
	ErrUnavailable = errors.New("distribution unavailable")
)

type Binding struct {
	ToolsetVersionID, ToolID, ToolVersion, ToolVersionID, BudgetID, ConnectionID, MCPName string
}

type Toolsets interface {
	ResolveBinding(context.Context, string, string, string, string) (Binding, error)
	ListDirectBindings(context.Context, string, string) ([]Binding, error)
}
