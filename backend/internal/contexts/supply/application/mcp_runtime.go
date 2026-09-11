package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

var (
	ErrMCPRouteUnavailable  = errors.New("MCP tool route unavailable")
	ErrMCPResultUnavailable = errors.New("MCP call result unavailable")
)

type MCPDiscoveryRef struct {
	WorkspaceID, SubjectID, ConnectionID, DeploymentRevision string
}

type PreparedMCPDiscovery struct {
	WorkspaceID string
	Deployment  domain.Deployment
	Credential  CredentialReference
	Secret      Secret
}

func (b *Broker) PrepareMCPDiscovery(ctx context.Context, ref MCPDiscoveryRef, at time.Time) (PreparedMCPDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return PreparedMCPDiscovery{}, err
	}
	if ref.WorkspaceID == "" || ref.SubjectID == "" || ref.ConnectionID == "" || !validDeploymentID(ref.DeploymentRevision) || at.IsZero() {
		return PreparedMCPDiscovery{}, ErrInvocationForbidden
	}
	deployment, err := b.deployments.FindDeployment(ctx, ref.DeploymentRevision)
	if err != nil {
		return PreparedMCPDiscovery{}, err
	}
	if !deployment.SupportsMCPTools() || deployment.State != "active" {
		return PreparedMCPDiscovery{}, ErrInvocationForbidden
	}
	credential, err := b.credentials.ResolveCredential(ctx, ref.WorkspaceID, ref.SubjectID, ref.ConnectionID, deployment.ProviderID, at)
	if err != nil {
		return PreparedMCPDiscovery{}, err
	}
	if credential.ConnectionID != ref.ConnectionID || credential.ProviderID != deployment.ProviderID || credential.CredentialVersionRef == "" || credential.Revision < 1 || !at.Before(credential.ValidUntil) {
		return PreparedMCPDiscovery{}, ErrInvocationForbidden
	}
	if deployment.AuthMode == "none" {
		return PreparedMCPDiscovery{WorkspaceID: ref.WorkspaceID, Deployment: deployment, Credential: credential}, nil
	}
	secret, err := b.secrets.ResolveSecret(ctx, SecretRequest{ProviderID: deployment.ProviderID, ConnectionID: credential.ConnectionID, CredentialVersionRef: credential.CredentialVersionRef, ConnectionRevision: credential.Revision})
	if err != nil || secret.Empty() {
		return PreparedMCPDiscovery{}, ErrSecretUnavailable
	}
	return PreparedMCPDiscovery{WorkspaceID: ref.WorkspaceID, Deployment: deployment, Credential: credential, Secret: secret}, nil
}

type MCPToolRouteRepository interface {
	ResolveMCPToolRoute(context.Context, string, string) (domain.MCPToolRoute, error)
}

type MCPCallResult struct {
	WorkspaceID, RunID, DeploymentRevision, SubmissionKey string
	ProviderID, ProviderRequestID                         string
	State                                                 string
	ResultJSON, ErrorCode                                 string
	ObservedAt                                            time.Time
}

func (r MCPCallResult) Validate() error {
	if r.WorkspaceID == "" || r.RunID == "" || !validDeploymentID(r.DeploymentRevision) || r.SubmissionKey == "" || !validDeploymentID(r.ProviderID) || r.ProviderRequestID == "" || r.ObservedAt.IsZero() {
		return ErrMCPResultUnavailable
	}
	if r.State == "succeeded" {
		if r.ResultJSON == "" || len(r.ResultJSON) > 1<<20 || !json.Valid([]byte(r.ResultJSON)) || r.ErrorCode != "" {
			return ErrMCPResultUnavailable
		}
		return nil
	}
	if r.State == "failed" && r.ResultJSON == "" && r.ErrorCode != "" {
		return nil
	}
	return ErrMCPResultUnavailable
}

type MCPCallResultRepository interface {
	SaveMCPCallResult(context.Context, MCPCallResult) error
	FindMCPCallResult(context.Context, string, string, string) (MCPCallResult, error)
}
