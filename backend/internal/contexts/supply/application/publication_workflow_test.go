package application

import (
	"context"
	"testing"
	"time"
)

type workflowIDs struct{ id string }

func (i workflowIDs) NewID() (string, error) { return i.id, nil }

type workflowClock struct{ at time.Time }

func (c workflowClock) Now() time.Time { return c.at }

type workflowRepository struct {
	submittedAt, expiresAt time.Time
	actor                  string
}

func workflowVersion(state string) PluginVersion {
	return PluginVersion{WorkspaceID: "ws_example", PluginID: "example.search", Version: "1.0.0", PublisherID: "publisher_example", Revision: 3, State: state}
}

func (r *workflowRepository) SubmitPluginPublication(_ context.Context, _, actor, _, _, _ string, at, expires time.Time) (PluginVersion, error) {
	r.actor, r.submittedAt, r.expiresAt = actor, at, expires
	return workflowVersion("submitted"), nil
}

func (r *workflowRepository) PublishPluginPublication(_ context.Context, _, actor, _, _ string, _ time.Time) (string, PluginVersion, error) {
	r.actor = actor
	return "plugin_approval_123", workflowVersion("published"), nil
}

func TestPublicationWorkflowBindsApprovalTTLAndOwnerActor(t *testing.T) {
	repository := &workflowRepository{}
	auth := &publisherAuthorizerStub{}
	at := time.Date(2026, 9, 15, 14, 30, 0, 0, time.UTC)
	workflow, err := NewPublicationWorkflow(repository, auth, workflowIDs{id: "plugin_approval_123"}, workflowClock{at: at})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.Submit(context.Background(), PublisherActor{UserID: "user_owner"}, "ws_example", "example.search", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if result.ApprovalID != "plugin_approval_123" || result.PluginVersion.State != "submitted" || repository.actor != "user_owner" {
		t.Fatalf("unexpected submission: %#v repository=%#v", result, repository)
	}
	if !repository.submittedAt.Equal(at) || !repository.expiresAt.Equal(at.Add(24*time.Hour)) {
		t.Fatalf("unexpected approval TTL: %s %s", repository.submittedAt, repository.expiresAt)
	}
}

func TestPublicationWorkflowPublishesOnlyServerConfirmedState(t *testing.T) {
	repository := &workflowRepository{}
	auth := &publisherAuthorizerStub{}
	workflow, _ := NewPublicationWorkflow(repository, auth, workflowIDs{id: "plugin_approval_unused"}, workflowClock{at: time.Date(2026, 9, 15, 15, 0, 0, 0, time.UTC)})
	result, err := workflow.Publish(context.Background(), PublisherActor{UserID: "user_owner"}, "ws_example", "example.search", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if result.ApprovalID != "plugin_approval_123" || result.PluginVersion.State != "published" {
		t.Fatalf("unexpected publication: %#v", result)
	}
}
