package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

const publisherTestDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type publisherDigesterStub struct{ err error }

func (d publisherDigesterStub) SHA256([]byte) (string, error) {
	if d.err != nil {
		return "", d.err
	}
	return publisherTestDigest, nil
}

type publisherAuthorizerStub struct {
	action string
	err    error
}

func (a *publisherAuthorizerStub) Authenticate(context.Context, string) (PublisherActor, error) {
	return PublisherActor{UserID: "user_owner"}, nil
}

func (a *publisherAuthorizerStub) AuthenticateMutation(context.Context, string, string) (PublisherActor, error) {
	return PublisherActor{UserID: "user_owner"}, nil
}

func (a *publisherAuthorizerStub) Authorize(_ context.Context, _ PublisherActor, _ string, action string) error {
	a.action = action
	return a.err
}

type publisherRepositoryStub struct {
	createdActor  string
	createdDigest string
	created       PluginManifest
	createCalls   int
}

func (r *publisherRepositoryStub) Snapshot(context.Context, string) (PublisherSnapshot, error) {
	return PublisherSnapshot{}, nil
}

func (r *publisherRepositoryStub) CreatePublisher(context.Context, string, string, string, string) (Publisher, error) {
	return Publisher{}, nil
}

func (r *publisherRepositoryStub) UpdatePublisher(context.Context, string, string, string, string) (Publisher, error) {
	return Publisher{}, nil
}

func (r *publisherRepositoryStub) CreatePlugin(context.Context, string, string, string, string) (Plugin, error) {
	return Plugin{}, nil
}

func (r *publisherRepositoryStub) CreatePluginVersion(_ context.Context, workspace, actor string, manifest PluginManifest, digest string) (PluginVersion, error) {
	r.createCalls++
	r.createdActor = actor
	r.createdDigest = digest
	r.created = manifest
	return PluginVersion{
		WorkspaceID:     workspace,
		PluginID:        manifest.PluginID,
		Version:         manifest.Version,
		PublisherID:     manifest.PublisherID,
		Revision:        1,
		State:           "draft",
		Manifest:        manifest,
		ManifestSHA256:  digest,
		CreatedByUserID: actor,
		CreatedAt:       time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC),
	}, nil
}

func (r *publisherRepositoryStub) UpdatePluginVersion(context.Context, string, string, string, string, PluginManifest, string) (PluginVersion, error) {
	return PluginVersion{}, nil
}

func (r *publisherRepositoryStub) PluginVersionPreflight(context.Context, string, string, string, string) (PluginPreflight, error) {
	return PluginPreflight{Ready: true, Issues: []PublicationIssue{}}, nil
}

func testPublisherManifest() PluginManifest {
	return PluginManifest{
		APIVersion:  "mender.io/plugin/v1alpha1",
		PluginID:    "example.search",
		Version:     "1.0.0",
		PublisherID: "publisher_example",
		DisplayName: "Example search",
		Description: "Reviewed capability references only.",
		Capabilities: []CapabilityReference{
			{Kind: "api_tool", ToolVersionID: "toolv_search_1"},
		},
	}
}

func TestPublisherServiceComputesManifestDigestAndUsesManageAuthority(t *testing.T) {
	repository := &publisherRepositoryStub{}
	authorizer := &publisherAuthorizerStub{}
	service, err := NewPublisherService(repository, authorizer, publisherDigesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := testPublisherManifest()
	value, err := service.CreatePluginVersion(context.Background(), PublisherActor{UserID: "user_owner"}, "ws_example", manifest)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer.action != "publisher:manage" {
		t.Fatalf("unexpected authorization action %q", authorizer.action)
	}
	if repository.createdActor != "user_owner" || repository.createdDigest != publisherTestDigest || !reflect.DeepEqual(repository.created, manifest) {
		t.Fatalf("server-owned manifest binding was not preserved: %#v", repository)
	}
	if value.State != "draft" || value.Revision != 1 || value.ManifestSHA256 != publisherTestDigest {
		t.Fatalf("unexpected created projection: %#v", value)
	}
}

func TestPublisherServiceRejectsManifestIdentityMismatchBeforeRepository(t *testing.T) {
	repository := &publisherRepositoryStub{}
	authorizer := &publisherAuthorizerStub{}
	service, _ := NewPublisherService(repository, authorizer, publisherDigesterStub{})
	manifest := testPublisherManifest()
	manifest.PluginID = "other.search"
	_, err := service.UpdatePluginVersion(context.Background(), PublisherActor{UserID: "user_owner"}, "ws_example", "example.search", "1.0.0", manifest)
	if !errors.Is(err, ErrPublicationInvalid) {
		t.Fatalf("expected invalid manifest identity, got %v", err)
	}
	if repository.createCalls != 0 || authorizer.action != "" {
		t.Fatal("invalid manifest reached authorization or repository")
	}
}

func TestPublisherServicePropagatesAuthorizationFailure(t *testing.T) {
	repository := &publisherRepositoryStub{}
	authorizer := &publisherAuthorizerStub{err: ErrPublicationForbidden}
	service, _ := NewPublisherService(repository, authorizer, publisherDigesterStub{})
	_, err := service.CreatePluginVersion(context.Background(), PublisherActor{UserID: "user_owner"}, "ws_example", testPublisherManifest())
	if !errors.Is(err, ErrPublicationForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatal("forbidden request reached repository")
	}
}
