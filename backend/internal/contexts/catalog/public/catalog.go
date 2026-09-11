package public

import (
	"context"
	"errors"
)

var (
	ErrNotFound    = errors.New("tool version unavailable")
	ErrUnavailable = errors.New("catalog unavailable")
)

type ToolVersion struct {
	ID, ToolID, Version, ProviderID, PriceVersionID, DeploymentRevision string
	Title, Description, InputSchema, OutputSchema                       string
	SideEffect, Idempotency                                             string
	MCPPublishable                                                      bool
}

type Catalog interface {
	ResolveToolVersion(context.Context, string, string, string) (ToolVersion, error)
}
