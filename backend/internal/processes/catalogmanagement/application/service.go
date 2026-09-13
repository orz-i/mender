package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrUnauthenticated = errors.New("catalog management authentication required")
	ErrForbidden       = errors.New("catalog management forbidden")
	ErrInvalid         = errors.New("catalog management invalid argument")
	ErrNotFound        = errors.New("catalog management record not found")
	ErrConflict        = errors.New("catalog management conflict")
	ErrPolicyDenied    = errors.New("catalog publication policy denied")
	ErrUnavailable     = errors.New("catalog management unavailable")
)

type Actor struct{ UserID string }

type Authorizer interface {
	Authenticate(context.Context, string) (Actor, error)
	AuthenticateMutation(context.Context, string, string) (Actor, error)
	Authorize(context.Context, Actor, string, string) error
}

type PolicyDecision struct {
	Sequence             int64
	PolicyRevisionID     string
	PolicyRevision       int64
	TargetKind, TargetID string
	TargetRevision       int64
	RiskLevel, Outcome   string
	ReasonCodes          []string
	EvaluatedAt          time.Time
}

type PublicationSubmission struct {
	Approval       *PublicationApproval
	PolicyDecision PolicyDecision
}

type PublicationApproval struct {
	WorkspaceID, ID, TargetKind, TargetID, RequesterUserID, ReviewerUserID, State, DecisionNote string
	TargetRevision                                                                              int64
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt                                              time.Time
}

type Clock interface{ Now() time.Time }
type IDGenerator interface{ NewID() (string, error) }

type ToolVersion struct {
	WorkspaceID, ToolVersionID, ToolID, Version, ProviderID, PriceVersionID, DeploymentRevision string
	Title, Description, InputSchema, OutputSchema, SideEffect, Idempotency                      string
	MCPPublishable                                                                              bool
	Revision                                                                                    int64
	State                                                                                       string
	CreatedAt, UpdatedAt, PublishedAt, RetiredAt                                                time.Time
}

type ToolVersionInput struct {
	ToolVersionID, ToolID, Version, ProviderID, PriceVersionID, DeploymentRevision string
	Title, Description, InputSchema, OutputSchema, SideEffect, Idempotency         string
	MCPPublishable                                                                 bool
}

type Binding struct {
	WorkspaceID, ToolsetID, ToolID, ToolVersionLabel, ToolVersionID, BudgetID string
	ConnectionID, MCPName                                                     string
	MCPExposed                                                                bool
	State                                                                     string
	PublishedAt                                                               time.Time
}

type BindingInput struct {
	ToolID, ToolVersionLabel, ToolVersionID, BudgetID, ConnectionID, MCPName string
	MCPExposed                                                               bool
}

type Toolset struct {
	WorkspaceID, ID                              string
	Revision                                     int64
	State                                        string
	CreatedAt, UpdatedAt, PublishedAt, RetiredAt time.Time
	Bindings                                     []Binding
}

type ConnectionOption struct {
	ConnectionID, ProviderID, State string
	Revision                        int64
	CreatedAt, ExpiresAt            time.Time
}

type PriceOption struct {
	ID, ToolVersionID, Currency string
	ReserveMicro                int64
	StartsAt, EndsAt            time.Time
	Active                      bool
}

type BudgetOption struct {
	BudgetID, PeriodID, Currency string
	StartsAt, EndsAt             time.Time
	Active                       bool
}

type Snapshot struct {
	ToolVersions []ToolVersion
	Toolsets     []Toolset
	Connections  []ConnectionOption
	Prices       []PriceOption
	Budgets      []BudgetOption
	Approvals    []PublicationApproval
}

type Issue struct{ Code, TargetID string }
type Preflight struct {
	Ready  bool
	Issues []Issue
}

type Repository interface {
	Snapshot(context.Context, string, time.Time) (Snapshot, error)
	CreateToolVersion(context.Context, string, ToolVersionInput) (ToolVersion, error)
	UpdateToolVersion(context.Context, string, string, ToolVersionInput) (ToolVersion, error)
	ToolVersionPreflight(context.Context, string, string, time.Time) (Preflight, error)
	PublishToolVersion(context.Context, string, string, time.Time) (ToolVersion, error)
	RetireToolVersion(context.Context, string, string, time.Time) (ToolVersion, error)
	CreateToolset(context.Context, string, string) (Toolset, error)
	UpsertBinding(context.Context, string, string, BindingInput) (Binding, error)
	DeleteBinding(context.Context, string, string, string) error
	ToolsetPreflight(context.Context, string, string, time.Time) (Preflight, error)
	PublishToolset(context.Context, string, string, time.Time) (Toolset, error)
	RetireToolset(context.Context, string, string, time.Time) (Toolset, error)
	SubmitPublication(context.Context, string, string, string, string, string, time.Time, time.Time) (PublicationSubmission, error)
}

type Service struct {
	repository Repository
	auth       Authorizer
	clock      Clock
	ids        IDGenerator
}

func New(repository Repository, auth Authorizer, clock Clock, generators ...IDGenerator) (*Service, error) {
	if repository == nil || auth == nil || clock == nil {
		return nil, ErrUnavailable
	}
	var ids IDGenerator
	if len(generators) > 1 {
		return nil, ErrUnavailable
	}
	if len(generators) == 1 {
		ids = generators[0]
	}
	return &Service{repository: repository, auth: auth, clock: clock, ids: ids}, nil
}

func validID(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validVersion(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for i, c := range v {
		if i == 0 && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func objectSchema(raw string, requireObject bool) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || !json.Valid([]byte(raw)) {
		return false
	}
	var value map[string]any
	if json.Unmarshal([]byte(raw), &value) != nil || value == nil {
		return false
	}
	if requireObject {
		kind, ok := value["type"].(string)
		return ok && kind == "object"
	}
	return true
}

func validMCPName(v string) bool {
	if len(v) < 1 || len(v) > 64 || v[0] < 'a' || v[0] > 'z' {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func validateToolInput(v ToolVersionInput) bool {
	return validID(v.ToolVersionID) && validID(v.ToolID) && validVersion(v.Version) && validID(v.ProviderID) &&
		validID(v.PriceVersionID) && validID(v.DeploymentRevision) && len([]rune(v.Title)) >= 1 && len([]rune(v.Title)) <= 200 &&
		len([]rune(v.Description)) <= 4000 && !strings.ContainsRune(v.Title, 0) && !strings.ContainsRune(v.Description, 0) &&
		objectSchema(v.InputSchema, true) && objectSchema(v.OutputSchema, false) &&
		(v.SideEffect == "read_only" || v.SideEffect == "write") &&
		(v.Idempotency == "safe_read" || v.Idempotency == "idempotent" || v.Idempotency == "unsafe")
}

func validateBinding(v BindingInput) bool {
	if !validID(v.ToolID) || !validVersion(v.ToolVersionLabel) || !validID(v.ToolVersionID) || !validID(v.BudgetID) || !validID(v.ConnectionID) {
		return false
	}
	if v.MCPExposed {
		return validMCPName(v.MCPName)
	}
	return v.MCPName == "" || validMCPName(v.MCPName)
}

func (s *Service) now() (time.Time, error) {
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return time.Time{}, ErrUnavailable
	}
	return now, nil
}

func (s *Service) Snapshot(ctx context.Context, actor Actor, workspace string) (Snapshot, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return Snapshot{}, ErrForbidden
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:read"); err != nil {
		return Snapshot{}, err
	}
	now, err := s.now()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := s.repository.Snapshot(ctx, workspace, now)
	if err != nil {
		return Snapshot{}, err
	}
	toolRevisions := make(map[string]int64, len(snapshot.ToolVersions))
	for _, tool := range snapshot.ToolVersions {
		toolRevisions[tool.ToolVersionID] = tool.Revision
	}
	toolsetRevisions := make(map[string]int64, len(snapshot.Toolsets))
	for _, toolset := range snapshot.Toolsets {
		toolsetRevisions[toolset.ID] = toolset.Revision
	}
	for i := range snapshot.Approvals {
		approval := &snapshot.Approvals[i]
		if approval.State != "pending" && approval.State != "approved" {
			continue
		}
		var revision int64
		var ok bool
		switch approval.TargetKind {
		case "tool_version":
			revision, ok = toolRevisions[approval.TargetID]
		case "toolset":
			revision, ok = toolsetRevisions[approval.TargetID]
		}
		if !ok || revision != approval.TargetRevision {
			approval.State = "expired"
		}
	}
	return snapshot, nil
}

func (s *Service) CreateToolVersion(ctx context.Context, actor Actor, workspace string, in ToolVersionInput) (ToolVersion, error) {
	if !validID(workspace) || !validateToolInput(in) {
		return ToolVersion{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return ToolVersion{}, err
	}
	return s.repository.CreateToolVersion(ctx, workspace, in)
}

func (s *Service) UpdateToolVersion(ctx context.Context, actor Actor, workspace, id string, in ToolVersionInput) (ToolVersion, error) {
	if !validID(workspace) || !validID(id) || id != in.ToolVersionID || !validateToolInput(in) {
		return ToolVersion{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return ToolVersion{}, err
	}
	return s.repository.UpdateToolVersion(ctx, workspace, id, in)
}

func (s *Service) ToolVersionPreflight(ctx context.Context, actor Actor, workspace, id string) (Preflight, error) {
	if !validID(workspace) || !validID(id) {
		return Preflight{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return Preflight{}, err
	}
	now, err := s.now()
	if err != nil {
		return Preflight{}, err
	}
	return s.repository.ToolVersionPreflight(ctx, workspace, id, now)
}

func (s *Service) PublishToolVersion(ctx context.Context, actor Actor, workspace, id string) (ToolVersion, error) {
	if !validID(workspace) || !validID(id) {
		return ToolVersion{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return ToolVersion{}, err
	}
	now, err := s.now()
	if err != nil {
		return ToolVersion{}, err
	}
	v, err := s.repository.PublishToolVersion(ctx, workspace, id, now)
	if err != nil {
		return ToolVersion{}, err
	}
	if v.State != "published" {
		return ToolVersion{}, ErrConflict
	}
	return v, nil
}

func (s *Service) RetireToolVersion(ctx context.Context, actor Actor, workspace, id string) (ToolVersion, error) {
	if !validID(workspace) || !validID(id) {
		return ToolVersion{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return ToolVersion{}, err
	}
	now, err := s.now()
	if err != nil {
		return ToolVersion{}, err
	}
	return s.repository.RetireToolVersion(ctx, workspace, id, now)
}

func (s *Service) CreateToolset(ctx context.Context, actor Actor, workspace, id string) (Toolset, error) {
	if !validID(workspace) || !validID(id) {
		return Toolset{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return Toolset{}, err
	}
	return s.repository.CreateToolset(ctx, workspace, id)
}

func (s *Service) UpsertBinding(ctx context.Context, actor Actor, workspace, toolset string, in BindingInput) (Binding, error) {
	if !validID(workspace) || !validID(toolset) || !validateBinding(in) {
		return Binding{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return Binding{}, err
	}
	return s.repository.UpsertBinding(ctx, workspace, toolset, in)
}

func (s *Service) DeleteBinding(ctx context.Context, actor Actor, workspace, toolset, toolVersion string) error {
	if !validID(workspace) || !validID(toolset) || !validID(toolVersion) {
		return ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return err
	}
	return s.repository.DeleteBinding(ctx, workspace, toolset, toolVersion)
}

func (s *Service) ToolsetPreflight(ctx context.Context, actor Actor, workspace, id string) (Preflight, error) {
	if !validID(workspace) || !validID(id) {
		return Preflight{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return Preflight{}, err
	}
	now, err := s.now()
	if err != nil {
		return Preflight{}, err
	}
	return s.repository.ToolsetPreflight(ctx, workspace, id, now)
}

func (s *Service) PublishToolset(ctx context.Context, actor Actor, workspace, id string) (Toolset, error) {
	if !validID(workspace) || !validID(id) {
		return Toolset{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return Toolset{}, err
	}
	now, err := s.now()
	if err != nil {
		return Toolset{}, err
	}
	v, err := s.repository.PublishToolset(ctx, workspace, id, now)
	if err != nil {
		return Toolset{}, err
	}
	if v.State != "published" {
		return Toolset{}, ErrConflict
	}
	return v, nil
}

func (s *Service) RetireToolset(ctx context.Context, actor Actor, workspace, id string) (Toolset, error) {
	if !validID(workspace) || !validID(id) {
		return Toolset{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return Toolset{}, err
	}
	now, err := s.now()
	if err != nil {
		return Toolset{}, err
	}
	return s.repository.RetireToolset(ctx, workspace, id, now)
}

func validPolicyDecision(v PolicyDecision, workspace, kind, id string) bool {
	if v.Sequence < 1 || !validID(v.PolicyRevisionID) || v.PolicyRevision < 1 || v.TargetKind != kind || v.TargetID != id || v.TargetRevision < 1 || v.EvaluatedAt.IsZero() {
		return false
	}
	if v.RiskLevel != "low" && v.RiskLevel != "medium" && v.RiskLevel != "high" && v.RiskLevel != "critical" {
		return false
	}
	if v.Outcome != "allow" && v.Outcome != "deny" || len(v.ReasonCodes) == 0 {
		return false
	}
	return validID(workspace)
}

func (s *Service) SubmitPublication(ctx context.Context, actor Actor, workspace, kind, id string) (PublicationSubmission, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || (kind != "tool_version" && kind != "toolset") {
		return PublicationSubmission{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:manage"); err != nil {
		return PublicationSubmission{}, err
	}
	if s.ids == nil {
		return PublicationSubmission{}, ErrUnavailable
	}
	requestID, err := s.ids.NewID()
	if err != nil || !validID(requestID) {
		return PublicationSubmission{}, ErrUnavailable
	}
	now, err := s.now()
	if err != nil {
		return PublicationSubmission{}, err
	}
	result, err := s.repository.SubmitPublication(ctx, workspace, requestID, kind, id, actor.UserID, now, now.Add(30*time.Minute))
	if err != nil {
		return PublicationSubmission{}, err
	}
	if !validPolicyDecision(result.PolicyDecision, workspace, kind, id) {
		return PublicationSubmission{}, ErrUnavailable
	}
	if result.PolicyDecision.Outcome == "deny" {
		if result.Approval != nil {
			return PublicationSubmission{}, ErrUnavailable
		}
		return result, ErrPolicyDenied
	}
	if result.Approval == nil || result.Approval.WorkspaceID != workspace || result.Approval.ID != requestID || result.Approval.TargetKind != kind || result.Approval.TargetID != id || result.Approval.State != "pending" {
		return PublicationSubmission{}, ErrUnavailable
	}
	return result, nil
}
