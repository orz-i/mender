package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

var (
	ErrInvocationForbidden   = errors.New("supplier invocation forbidden")
	ErrInvocationUnavailable = errors.New("supplier invocation unavailable")
	ErrSecretUnavailable     = errors.New("supplier credential secret unavailable")
)

type InvocationRef struct{ WorkspaceID, RunID string }

type ExecutionInput struct {
	WorkspaceID, RunID, SubjectID, ConnectionID, ToolVersionID, DeploymentRevision string
	CanonicalArguments                                                             string
}

type CredentialReference struct {
	ConnectionID, ProviderID, CredentialVersionRef string
	Revision                                       int64
	ValidUntil                                     time.Time
}

type ExecutionInputSource interface {
	ResolveExecutionInput(context.Context, string, string) (ExecutionInput, error)
}

type CredentialSource interface {
	ResolveCredential(context.Context, string, string, string, string, time.Time) (CredentialReference, error)
}

type DeploymentRepository interface {
	FindDeployment(context.Context, string) (domain.Deployment, error)
}

type SecretRequest struct {
	ProviderID, ConnectionID, CredentialVersionRef string
	ConnectionRevision                             int64
}

type SecretProvider interface {
	ResolveSecret(context.Context, SecretRequest) (Secret, error)
}

type Secret struct{ value []byte }

func NewSecret(value []byte) (Secret, error) {
	if len(value) < 1 || len(value) > 16<<10 {
		return Secret{}, ErrSecretUnavailable
	}
	copyOf := append([]byte(nil), value...)
	return Secret{value: copyOf}, nil
}

func (s Secret) Bytes() []byte    { return append([]byte(nil), s.value...) }
func (s Secret) Empty() bool      { return len(s.value) == 0 }
func (s Secret) String() string   { return "[REDACTED]" }
func (s Secret) GoString() string { return "[REDACTED]" }

type PreparedInvocation struct {
	WorkspaceID, RunID, ToolVersionID string
	Deployment                        domain.Deployment
	CanonicalArguments                string
	Credential                        CredentialReference
	Secret                            Secret
}

type Broker struct {
	inputs      ExecutionInputSource
	credentials CredentialSource
	deployments DeploymentRepository
	secrets     SecretProvider
}

func NewBroker(inputs ExecutionInputSource, credentials CredentialSource, deployments DeploymentRepository, secrets SecretProvider) (*Broker, error) {
	if inputs == nil || credentials == nil || deployments == nil || secrets == nil {
		return nil, ErrInvocationUnavailable
	}
	return &Broker{inputs: inputs, credentials: credentials, deployments: deployments, secrets: secrets}, nil
}

func (b *Broker) Prepare(ctx context.Context, ref InvocationRef, at time.Time) (PreparedInvocation, error) {
	if err := ctx.Err(); err != nil {
		return PreparedInvocation{}, err
	}
	if ref.WorkspaceID == "" || ref.RunID == "" || at.IsZero() {
		return PreparedInvocation{}, ErrInvocationForbidden
	}
	input, err := b.inputs.ResolveExecutionInput(ctx, ref.WorkspaceID, ref.RunID)
	if err != nil {
		return PreparedInvocation{}, err
	}
	if input.WorkspaceID != ref.WorkspaceID || input.RunID != ref.RunID || input.SubjectID == "" || input.ConnectionID == "" || input.ToolVersionID == "" || input.DeploymentRevision == "" || len(input.CanonicalArguments) < 2 {
		return PreparedInvocation{}, ErrInvocationUnavailable
	}
	deployment, err := b.deployments.FindDeployment(ctx, input.DeploymentRevision)
	if err != nil {
		return PreparedInvocation{}, err
	}
	if deployment.Validate() != nil || deployment.State != "active" || len(input.CanonicalArguments) > deployment.MaxRequestBytes {
		return PreparedInvocation{}, ErrInvocationForbidden
	}
	credential, err := b.credentials.ResolveCredential(ctx, ref.WorkspaceID, input.SubjectID, input.ConnectionID, deployment.ProviderID, at)
	if err != nil {
		return PreparedInvocation{}, err
	}
	if credential.ConnectionID != input.ConnectionID || credential.ProviderID != deployment.ProviderID || credential.CredentialVersionRef == "" || credential.Revision < 1 || !at.Before(credential.ValidUntil) {
		return PreparedInvocation{}, ErrInvocationForbidden
	}
	prepared := PreparedInvocation{WorkspaceID: ref.WorkspaceID, RunID: ref.RunID, ToolVersionID: input.ToolVersionID, Deployment: deployment, CanonicalArguments: input.CanonicalArguments, Credential: credential}
	if deployment.AuthMode == "none" {
		return prepared, nil
	}
	secret, err := b.secrets.ResolveSecret(ctx, SecretRequest{ProviderID: deployment.ProviderID, ConnectionID: credential.ConnectionID, CredentialVersionRef: credential.CredentialVersionRef, ConnectionRevision: credential.Revision})
	if err != nil || secret.Empty() {
		return PreparedInvocation{}, ErrSecretUnavailable
	}
	prepared.Secret = secret
	return prepared, nil
}
