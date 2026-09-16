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
	ErrProviderQuarantined   = errors.New("provider quarantined")
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
	ProviderAcceptsNewWork(context.Context, string) error
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

type PreparedControl struct {
	WorkspaceID, RunID string
	Deployment         domain.Deployment
	Credential         CredentialReference
	Secret             Secret
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

func (b *Broker) resolve(ctx context.Context, ref InvocationRef, at time.Time, allowDisabled, enforceRequestLimit bool) (ExecutionInput, domain.Deployment, CredentialReference, Secret, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, err
	}
	if ref.WorkspaceID == "" || ref.RunID == "" || at.IsZero() {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrInvocationForbidden
	}
	input, err := b.inputs.ResolveExecutionInput(ctx, ref.WorkspaceID, ref.RunID)
	if err != nil {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, err
	}
	if input.WorkspaceID != ref.WorkspaceID || input.RunID != ref.RunID || input.SubjectID == "" || input.ConnectionID == "" || input.ToolVersionID == "" || input.DeploymentRevision == "" || len(input.CanonicalArguments) < 2 {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrInvocationUnavailable
	}
	deployment, err := b.deployments.FindDeployment(ctx, input.DeploymentRevision)
	if err != nil {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, err
	}
	if deployment.Validate() != nil || (!allowDisabled && deployment.State != "active") {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrInvocationForbidden
	}
	if !allowDisabled {
		if err = b.deployments.ProviderAcceptsNewWork(ctx, deployment.ProviderID); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, err
			}
			return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrInvocationForbidden
		}
	}
	if enforceRequestLimit && len(input.CanonicalArguments) > deployment.MaxRequestBytes {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrInvocationForbidden
	}
	credential, err := b.credentials.ResolveCredential(ctx, ref.WorkspaceID, input.SubjectID, input.ConnectionID, deployment.ProviderID, at)
	if err != nil {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, err
	}
	if credential.ConnectionID != input.ConnectionID || credential.ProviderID != deployment.ProviderID || credential.CredentialVersionRef == "" || credential.Revision < 1 || !at.Before(credential.ValidUntil) {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrInvocationForbidden
	}
	if deployment.AuthMode == "none" {
		return input, deployment, credential, Secret{}, nil
	}
	secret, err := b.secrets.ResolveSecret(ctx, SecretRequest{ProviderID: deployment.ProviderID, ConnectionID: credential.ConnectionID, CredentialVersionRef: credential.CredentialVersionRef, ConnectionRevision: credential.Revision})
	if err != nil || secret.Empty() {
		return ExecutionInput{}, domain.Deployment{}, CredentialReference{}, Secret{}, ErrSecretUnavailable
	}
	return input, deployment, credential, secret, nil
}

func (b *Broker) Prepare(ctx context.Context, ref InvocationRef, at time.Time) (PreparedInvocation, error) {
	input, deployment, credential, secret, err := b.resolve(ctx, ref, at, false, true)
	if err != nil {
		return PreparedInvocation{}, err
	}
	return PreparedInvocation{
		WorkspaceID: ref.WorkspaceID, RunID: ref.RunID, ToolVersionID: input.ToolVersionID,
		Deployment: deployment, CanonicalArguments: input.CanonicalArguments, Credential: credential, Secret: secret,
	}, nil
}

// PrepareControl resolves the same immutable admitted deployment and current
// connection credential as submission, but does not expose admitted arguments
// to provider status/cancel adapters. Disabled deployments may still reconcile
// already-submitted work; egress policy remains the independent kill switch.
func (b *Broker) PrepareControl(ctx context.Context, ref InvocationRef, at time.Time) (PreparedControl, error) {
	_, deployment, credential, secret, err := b.resolve(ctx, ref, at, true, false)
	if err != nil {
		return PreparedControl{}, err
	}
	return PreparedControl{WorkspaceID: ref.WorkspaceID, RunID: ref.RunID, Deployment: deployment, Credential: credential, Secret: secret}, nil
}
