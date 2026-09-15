package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

var (
	ErrPublicationUnauthenticated = errors.New("publisher authentication required")
	ErrPublicationForbidden       = errors.New("publisher operation forbidden")
	ErrPublicationInvalid         = errors.New("publisher request invalid")
	ErrPublicationNotFound        = errors.New("publisher record not found")
	ErrPublicationConflict        = errors.New("publisher state conflict")
	ErrPublicationUnavailable     = errors.New("publisher management unavailable")
)

type PublisherActor struct{ UserID string }

type PublisherAuthorizer interface {
	Authenticate(context.Context, string) (PublisherActor, error)
	AuthenticateMutation(context.Context, string, string) (PublisherActor, error)
	Authorize(context.Context, PublisherActor, string, string) error
}

type ManifestDigester interface {
	SHA256([]byte) (string, error)
}

type CapabilityReference struct {
	Kind               string `json:"kind"`
	ToolVersionID      string `json:"tool_version_id,omitempty"`
	DeploymentRevision string `json:"deployment_revision,omitempty"`
}

type PluginManifest struct {
	APIVersion   string                `json:"apiVersion"`
	PluginID     string                `json:"plugin_id"`
	Version      string                `json:"version"`
	PublisherID  string                `json:"publisher_id"`
	DisplayName  string                `json:"display_name"`
	Description  string                `json:"description"`
	Capabilities []CapabilityReference `json:"capabilities"`
}

func (m PluginManifest) Domain() domain.PluginManifest {
	capabilities := make([]domain.CapabilityReference, 0, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		capabilities = append(capabilities, domain.CapabilityReference{
			Kind:               domain.CapabilityKind(capability.Kind),
			ToolVersionID:      capability.ToolVersionID,
			DeploymentRevision: capability.DeploymentRevision,
		})
	}
	return domain.PluginManifest{
		APIVersion:   m.APIVersion,
		PluginID:     m.PluginID,
		Version:      m.Version,
		PublisherID:  m.PublisherID,
		DisplayName:  m.DisplayName,
		Description:  m.Description,
		Capabilities: capabilities,
	}
}

func ProjectPluginManifest(m domain.PluginManifest) PluginManifest {
	capabilities := make([]CapabilityReference, 0, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		capabilities = append(capabilities, CapabilityReference{
			Kind:               string(capability.Kind),
			ToolVersionID:      capability.ToolVersionID,
			DeploymentRevision: capability.DeploymentRevision,
		})
	}
	return PluginManifest{
		APIVersion:   m.APIVersion,
		PluginID:     m.PluginID,
		Version:      m.Version,
		PublisherID:  m.PublisherID,
		DisplayName:  m.DisplayName,
		Description:  m.Description,
		Capabilities: capabilities,
	}
}

type Publisher struct {
	WorkspaceID, ID, OwnerUserID, DisplayName, State string
	CreatedAt, UpdatedAt                             time.Time
}

type Plugin struct {
	WorkspaceID, ID, PublisherID, CreatedByUserID string
	CreatedAt                                     time.Time
}

type PluginVersion struct {
	WorkspaceID, PluginID, Version, PublisherID, State, ManifestSHA256, CreatedByUserID  string
	Revision                                                                             int64
	Manifest                                                                             PluginManifest
	CreatedAt, UpdatedAt, SubmittedAt, ApprovedAt, PublishedAt, DeprecatedAt, DisabledAt time.Time
}

type PublicationIssue struct{ Code, TargetID string }
type PluginPreflight struct {
	Ready  bool
	Issues []PublicationIssue
}

type PublisherSnapshot struct {
	Publishers     []Publisher
	Plugins        []Plugin
	PluginVersions []PluginVersion
}

type PublisherRepository interface {
	Snapshot(context.Context, string) (PublisherSnapshot, error)
	CreatePublisher(context.Context, string, string, string, string) (Publisher, error)
	UpdatePublisher(context.Context, string, string, string, string) (Publisher, error)
	CreatePlugin(context.Context, string, string, string, string) (Plugin, error)
	CreatePluginVersion(context.Context, string, string, PluginManifest, string) (PluginVersion, error)
	UpdatePluginVersion(context.Context, string, string, string, string, PluginManifest, string) (PluginVersion, error)
	PluginVersionPreflight(context.Context, string, string, string, string) (PluginPreflight, error)
}

type PublisherService struct {
	repository PublisherRepository
	authorizer PublisherAuthorizer
	digester   ManifestDigester
}

func NewPublisherService(repository PublisherRepository, authorizer PublisherAuthorizer, digester ManifestDigester) (*PublisherService, error) {
	if repository == nil || authorizer == nil || digester == nil {
		return nil, ErrPublicationUnavailable
	}
	return &PublisherService{repository: repository, authorizer: authorizer, digester: digester}, nil
}

func validPublisherID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func validPluginID(value string) bool {
	if len(value) < 3 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func validDisplayName(value string) bool {
	runes := []rune(value)
	return len(runes) >= 1 && len(runes) <= 200 && !strings.ContainsRune(value, 0)
}

func (s *PublisherService) Snapshot(ctx context.Context, actor PublisherActor, workspace string) (PublisherSnapshot, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) {
		return PublisherSnapshot{}, ErrPublicationForbidden
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "publisher:read"); err != nil {
		return PublisherSnapshot{}, err
	}
	return s.repository.Snapshot(ctx, workspace)
}

func (s *PublisherService) CreatePublisher(ctx context.Context, actor PublisherActor, workspace, publisherID, displayName string) (Publisher, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPublisherID(publisherID) || !validDisplayName(displayName) {
		return Publisher{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return Publisher{}, err
	}
	return s.repository.CreatePublisher(ctx, workspace, actor.UserID, publisherID, displayName)
}

func (s *PublisherService) UpdatePublisher(ctx context.Context, actor PublisherActor, workspace, publisherID, displayName string) (Publisher, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPublisherID(publisherID) || !validDisplayName(displayName) {
		return Publisher{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return Publisher{}, err
	}
	return s.repository.UpdatePublisher(ctx, workspace, actor.UserID, publisherID, displayName)
}

func (s *PublisherService) CreatePlugin(ctx context.Context, actor PublisherActor, workspace, publisherID, pluginID string) (Plugin, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPublisherID(publisherID) || !validPluginID(pluginID) {
		return Plugin{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return Plugin{}, err
	}
	return s.repository.CreatePlugin(ctx, workspace, actor.UserID, publisherID, pluginID)
}

func (s *PublisherService) prepareManifest(workspace, publisherID, pluginID, version string, manifest PluginManifest) (PluginManifest, string, error) {
	if !validPublisherID(workspace) || !validPublisherID(publisherID) || !validPluginID(pluginID) || manifest.PublisherID != publisherID || manifest.PluginID != pluginID || manifest.Version != version {
		return PluginManifest{}, "", ErrPublicationInvalid
	}
	domainManifest := manifest.Domain()
	body, err := domainManifest.CanonicalJSON()
	if err != nil {
		return PluginManifest{}, "", ErrPublicationInvalid
	}
	digest, err := s.digester.SHA256(body)
	if err != nil || len(digest) != 64 {
		return PluginManifest{}, "", ErrPublicationUnavailable
	}
	return manifest, digest, nil
}

func (s *PublisherService) CreatePluginVersion(ctx context.Context, actor PublisherActor, workspace string, manifest PluginManifest) (PluginVersion, error) {
	if !validPublisherID(actor.UserID) {
		return PluginVersion{}, ErrPublicationInvalid
	}
	manifest, digest, err := s.prepareManifest(workspace, manifest.PublisherID, manifest.PluginID, manifest.Version, manifest)
	if err != nil {
		return PluginVersion{}, err
	}
	if err = s.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return PluginVersion{}, err
	}
	return s.repository.CreatePluginVersion(ctx, workspace, actor.UserID, manifest, digest)
}

func (s *PublisherService) UpdatePluginVersion(ctx context.Context, actor PublisherActor, workspace, pluginID, version string, manifest PluginManifest) (PluginVersion, error) {
	if !validPublisherID(actor.UserID) {
		return PluginVersion{}, ErrPublicationInvalid
	}
	manifest, digest, err := s.prepareManifest(workspace, manifest.PublisherID, pluginID, version, manifest)
	if err != nil {
		return PluginVersion{}, err
	}
	if err = s.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return PluginVersion{}, err
	}
	return s.repository.UpdatePluginVersion(ctx, workspace, actor.UserID, pluginID, version, manifest, digest)
}

func (s *PublisherService) PluginVersionPreflight(ctx context.Context, actor PublisherActor, workspace, pluginID, version string) (PluginPreflight, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPluginID(pluginID) || version == "" || len(version) > 128 {
		return PluginPreflight{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "publisher:manage"); err != nil {
		return PluginPreflight{}, err
	}
	return s.repository.PluginVersionPreflight(ctx, workspace, actor.UserID, pluginID, version)
}
