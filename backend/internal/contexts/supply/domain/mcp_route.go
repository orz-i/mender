package domain

import (
	"errors"
	"time"
)

var ErrInvalidMCPToolRoute = errors.New("invalid MCP tool route")

type MCPToolRoute struct {
	ToolVersionID      string
	DeploymentRevision string
	UpstreamToolName   string
	SnapshotSHA256     string
	State              string
	CreatedAt          time.Time
}

func (r MCPToolRoute) Validate() error {
	if !validID(r.ToolVersionID) || !validID(r.DeploymentRevision) || !validMCPToolName(r.UpstreamToolName) || len(r.SnapshotSHA256) != 64 || r.CreatedAt.IsZero() || (r.State != "active" && r.State != "disabled") {
		return ErrInvalidMCPToolRoute
	}
	for _, c := range r.SnapshotSHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ErrInvalidMCPToolRoute
		}
	}
	return nil
}

func (r MCPToolRoute) Active() bool { return r.Validate() == nil && r.State == "active" }
