package application

import (
	"context"
	"time"
)

const pluginApprovalTTL = 24 * time.Hour

type PublicationIDGenerator interface{ NewID() (string, error) }
type PublicationClock interface{ Now() time.Time }

type PluginPublicationSubmission struct {
	ApprovalID    string
	PluginVersion PluginVersion
}

type PluginPublicationWorkflowRepository interface {
	SubmitPluginPublication(context.Context, string, string, string, string, string, time.Time, time.Time) (PluginVersion, error)
	PublishPluginPublication(context.Context, string, string, string, string, time.Time) (string, PluginVersion, error)
}

type PublicationWorkflow struct {
	repository PluginPublicationWorkflowRepository
	authorizer PublisherAuthorizer
	ids        PublicationIDGenerator
	clock      PublicationClock
}

func NewPublicationWorkflow(repository PluginPublicationWorkflowRepository, authorizer PublisherAuthorizer, ids PublicationIDGenerator, clock PublicationClock) (*PublicationWorkflow, error) {
	if repository == nil || authorizer == nil || ids == nil || clock == nil {
		return nil, ErrPublicationUnavailable
	}
	return &PublicationWorkflow{repository: repository, authorizer: authorizer, ids: ids, clock: clock}, nil
}

func (w *PublicationWorkflow) now() (time.Time, error) {
	at := w.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return time.Time{}, ErrPublicationUnavailable
	}
	return at, nil
}

func (w *PublicationWorkflow) Submit(ctx context.Context, actor PublisherActor, workspace, pluginID, version string) (PluginPublicationSubmission, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPluginID(pluginID) || version == "" || len(version) > 128 {
		return PluginPublicationSubmission{}, ErrPublicationInvalid
	}
	if err := w.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return PluginPublicationSubmission{}, err
	}
	id, err := w.ids.NewID()
	if err != nil || !validPublisherID(id) {
		return PluginPublicationSubmission{}, ErrPublicationUnavailable
	}
	at, err := w.now()
	if err != nil {
		return PluginPublicationSubmission{}, err
	}
	versionValue, err := w.repository.SubmitPluginPublication(ctx, workspace, actor.UserID, pluginID, version, id, at, at.Add(pluginApprovalTTL))
	if err != nil {
		return PluginPublicationSubmission{}, err
	}
	if versionValue.WorkspaceID != workspace || versionValue.PluginID != pluginID || versionValue.Version != version || versionValue.State != "submitted" || versionValue.Revision < 1 {
		return PluginPublicationSubmission{}, ErrPublicationUnavailable
	}
	return PluginPublicationSubmission{ApprovalID: id, PluginVersion: versionValue}, nil
}

func (w *PublicationWorkflow) Publish(ctx context.Context, actor PublisherActor, workspace, pluginID, version string) (PluginPublicationSubmission, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPluginID(pluginID) || version == "" || len(version) > 128 {
		return PluginPublicationSubmission{}, ErrPublicationInvalid
	}
	if err := w.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return PluginPublicationSubmission{}, err
	}
	at, err := w.now()
	if err != nil {
		return PluginPublicationSubmission{}, err
	}
	id, versionValue, err := w.repository.PublishPluginPublication(ctx, workspace, actor.UserID, pluginID, version, at)
	if err != nil {
		return PluginPublicationSubmission{}, err
	}
	if !validPublisherID(id) || versionValue.WorkspaceID != workspace || versionValue.PluginID != pluginID || versionValue.Version != version || versionValue.State != "published" || versionValue.Revision < 1 {
		return PluginPublicationSubmission{}, ErrPublicationUnavailable
	}
	return PluginPublicationSubmission{ApprovalID: id, PluginVersion: versionValue}, nil
}
