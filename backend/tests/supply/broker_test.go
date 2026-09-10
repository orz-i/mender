package supply_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

type inputSourceFunc func(context.Context, string, string) (application.ExecutionInput, error)

func (f inputSourceFunc) ResolveExecutionInput(ctx context.Context, workspace, run string) (application.ExecutionInput, error) {
	return f(ctx, workspace, run)
}

type credentialSourceFunc func(context.Context, string, string, string, string, time.Time) (application.CredentialReference, error)

func (f credentialSourceFunc) ResolveCredential(ctx context.Context, workspace, subject, connection, provider string, at time.Time) (application.CredentialReference, error) {
	return f(ctx, workspace, subject, connection, provider, at)
}

type deploymentRepoFunc func(context.Context, string) (domain.Deployment, error)

func (f deploymentRepoFunc) FindDeployment(ctx context.Context, revision string) (domain.Deployment, error) {
	return f(ctx, revision)
}

type secretProviderFunc func(context.Context, application.SecretRequest) (application.Secret, error)

func (f secretProviderFunc) ResolveSecret(ctx context.Context, request application.SecretRequest) (application.Secret, error) {
	return f(ctx, request)
}

func deployment(at time.Time, auth string) domain.Deployment {
	value := domain.Deployment{
		Revision: "deploy_a", ProviderID: "provider_a", TransportKind: "http",
		EndpointURL: "https://provider.invalid/v1/run", HTTPMethod: "POST", AuthMode: auth,
		IdempotencyHeader: "Idempotency-Key", RequestTimeout: 10 * time.Second,
		MaxRequestBytes: 1024, MaxResponseBytes: 4096, State: "active", CreatedAt: at.Add(-time.Hour),
	}
	if auth == "header" {
		value.AuthHeaderName = "X-Provider-Key"
	}
	return value
}

func TestBrokerRevalidatesCredentialAndKeepsSecretRedacted(t *testing.T) {
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	input := application.ExecutionInput{WorkspaceID: "ws_a", RunID: "run_a", SubjectID: "sa_a", ConnectionID: "conn_a", ToolVersionID: "toolv_a", DeploymentRevision: "deploy_a", CanonicalArguments: `{"q":"safe"}`}
	credential := application.CredentialReference{ConnectionID: "conn_a", ProviderID: "provider_a", CredentialVersionRef: "credv_a", Revision: 7, ValidUntil: at.Add(time.Hour)}
	var secretCalls int
	broker, err := application.NewBroker(
		inputSourceFunc(func(context.Context, string, string) (application.ExecutionInput, error) { return input, nil }),
		credentialSourceFunc(func(_ context.Context, workspace, subject, connection, provider string, got time.Time) (application.CredentialReference, error) {
			if workspace != "ws_a" || subject != "sa_a" || connection != "conn_a" || provider != "provider_a" || !got.Equal(at) {
				t.Fatalf("credential revalidation used wrong authority facts: %s %s %s %s %v", workspace, subject, connection, provider, got)
			}
			return credential, nil
		}),
		deploymentRepoFunc(func(_ context.Context, revision string) (domain.Deployment, error) {
			if revision != "deploy_a" {
				t.Fatalf("wrong deployment revision: %s", revision)
			}
			return deployment(at, "bearer"), nil
		}),
		secretProviderFunc(func(_ context.Context, request application.SecretRequest) (application.Secret, error) {
			secretCalls++
			if request.ProviderID != "provider_a" || request.ConnectionID != "conn_a" || request.CredentialVersionRef != "credv_a" || request.ConnectionRevision != 7 {
				t.Fatalf("secret provider received wrong opaque reference: %+v", request)
			}
			return application.NewSecret([]byte("super-secret-token"))
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := broker.Prepare(context.Background(), application.InvocationRef{WorkspaceID: "ws_a", RunID: "run_a"}, at)
	if err != nil || secretCalls != 1 || prepared.CanonicalArguments != input.CanonicalArguments || prepared.Secret.Empty() {
		t.Fatal(prepared, secretCalls, err)
	}
	if fmt.Sprint(prepared.Secret) != "[REDACTED]" || fmt.Sprintf("%#v", prepared.Secret) != "[REDACTED]" {
		t.Fatal("secret formatting leaked material")
	}
	if string(prepared.Secret.Bytes()) != "super-secret-token" {
		t.Fatal("secret bytes changed")
	}
}

func TestBrokerSkipsSecretForNoAuthAndFailsClosedBeforeSecret(t *testing.T) {
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	baseInput := application.ExecutionInput{WorkspaceID: "ws_a", RunID: "run_a", SubjectID: "sa_a", ConnectionID: "conn_a", ToolVersionID: "toolv_a", DeploymentRevision: "deploy_a", CanonicalArguments: `{}`}
	credential := application.CredentialReference{ConnectionID: "conn_a", ProviderID: "provider_a", CredentialVersionRef: "credv_a", Revision: 1, ValidUntil: at.Add(time.Hour)}
	secretCalls := 0
	newBroker := func(input application.ExecutionInput, d domain.Deployment, c application.CredentialReference) *application.Broker {
		b, err := application.NewBroker(
			inputSourceFunc(func(context.Context, string, string) (application.ExecutionInput, error) { return input, nil }),
			credentialSourceFunc(func(context.Context, string, string, string, string, time.Time) (application.CredentialReference, error) {
				return c, nil
			}),
			deploymentRepoFunc(func(context.Context, string) (domain.Deployment, error) { return d, nil }),
			secretProviderFunc(func(context.Context, application.SecretRequest) (application.Secret, error) {
				secretCalls++
				return application.NewSecret([]byte("should-not-be-called"))
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	prepared, err := newBroker(baseInput, deployment(at, "none"), credential).Prepare(context.Background(), application.InvocationRef{WorkspaceID: "ws_a", RunID: "run_a"}, at)
	if err != nil || !prepared.Secret.Empty() || secretCalls != 0 {
		t.Fatal(prepared, secretCalls, err)
	}

	wrongProvider := credential
	wrongProvider.ProviderID = "provider_other"
	_, err = newBroker(baseInput, deployment(at, "bearer"), wrongProvider).Prepare(context.Background(), application.InvocationRef{WorkspaceID: "ws_a", RunID: "run_a"}, at)
	if !errors.Is(err, application.ErrInvocationForbidden) || secretCalls != 0 {
		t.Fatal("provider mismatch reached secret provider", err, secretCalls)
	}

	oversized := baseInput
	oversized.CanonicalArguments = string(make([]byte, 2048))
	_, err = newBroker(oversized, deployment(at, "bearer"), credential).Prepare(context.Background(), application.InvocationRef{WorkspaceID: "ws_a", RunID: "run_a"}, at)
	if !errors.Is(err, application.ErrInvocationForbidden) || secretCalls != 0 {
		t.Fatal("oversized input reached secret provider", err, secretCalls)
	}
}

func TestBrokerPropagatesCredentialRevocationWithoutSecretAccess(t *testing.T) {
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	input := application.ExecutionInput{WorkspaceID: "ws_a", RunID: "run_a", SubjectID: "sa_a", ConnectionID: "conn_a", ToolVersionID: "toolv_a", DeploymentRevision: "deploy_a", CanonicalArguments: `{}`}
	secretCalls := 0
	broker, err := application.NewBroker(
		inputSourceFunc(func(context.Context, string, string) (application.ExecutionInput, error) { return input, nil }),
		credentialSourceFunc(func(context.Context, string, string, string, string, time.Time) (application.CredentialReference, error) {
			return application.CredentialReference{}, application.ErrInvocationForbidden
		}),
		deploymentRepoFunc(func(context.Context, string) (domain.Deployment, error) { return deployment(at, "header"), nil }),
		secretProviderFunc(func(context.Context, application.SecretRequest) (application.Secret, error) {
			secretCalls++
			return application.NewSecret([]byte("x"))
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Prepare(context.Background(), application.InvocationRef{WorkspaceID: "ws_a", RunID: "run_a"}, at)
	if !errors.Is(err, application.ErrInvocationForbidden) || secretCalls != 0 {
		t.Fatal(err, secretCalls)
	}
}
