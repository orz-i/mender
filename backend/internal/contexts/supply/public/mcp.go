package public

import (
	"context"
	"errors"
	"time"
)

var ErrMCPDiscoveryUnavailable = errors.New("MCP discovery unavailable")

type MCPToolCandidate struct {
	DeploymentRevision string
	ToolName           string
	Title              string
	Description        string
	InputSchema        string
	OutputSchema       string
	AnnotationsJSON    string
	ContentSHA256      string
	DiscoveredAt       time.Time
}

type MCPDiscoveryCatalog interface {
	ListMCPToolCandidates(context.Context, string) ([]MCPToolCandidate, error)
}
