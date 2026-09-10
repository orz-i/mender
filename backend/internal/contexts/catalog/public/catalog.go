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
}

type Catalog interface {
	ResolveToolVersion(context.Context, string, string, string) (ToolVersion, error)
}
