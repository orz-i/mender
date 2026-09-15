package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrNoAgentInput             = errors.New("no agent input request available")
	ErrAgentInputOutcomeUnknown = errors.New("agent input delivery outcome unknown")
)

type PreparedAgentInput struct {
	AnswerJSON   string
	AnswerSHA256 string
	SubmissionID string
}

func (s *AgentInputSubmissions) Get(ctx context.Context, caller ports.Caller, runID domain.RunID) (AgentInputSubmissionRecord, error) {
	if err := ctx.Err(); err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	if !caller.WorkspaceID.IsValid() || !runID.IsValid() || caller.SubjectID == "" || caller.CredentialID == "" {
		return AgentInputSubmissionRecord{}, ErrAgentInputInvalid
	}
	if err := s.authorizer.Authorize(ctx, caller, ports.ReadRun, runID); err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	record, err := s.repository.FindAgentInputPublic(ctx, caller.WorkspaceID, runID)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	if record.WorkspaceID != string(caller.WorkspaceID) || record.RunID != string(runID) || !validAgentInputID(record.InputRequestID, 200, true) || !validAgentInputPrompt(record.Prompt) || validateAgentInputSchema(record.InputSchemaJSON) != nil || record.RequestedAt.IsZero() || record.UpdatedAt.Before(record.RequestedAt) {
		return AgentInputSubmissionRecord{}, ErrAgentInputUnavailable
	}
	switch record.State {
	case "pending", "sending", "unknown", "submitted":
		return record, nil
	default:
		return AgentInputSubmissionRecord{}, ErrAgentInputUnavailable
	}
}

func (p PreparedAgentInput) Valid() bool {
	return len(p.AnswerJSON) >= 2 && len(p.AnswerJSON) <= 64<<10 && len(p.AnswerSHA256) == 64 && validAgentInputID(p.SubmissionID, 200, true)
}

type AgentInputPreparer interface {
	PrepareAgentInput(schemaJSON, inputRequestID string, raw []byte) (PreparedAgentInput, error)
}

type AgentInputSubmissionTarget struct {
	WorkspaceID       domain.WorkspaceID
	RunID             domain.RunID
	AttemptNo         uint32
	ProviderID        string
	ProviderRequestID string
	ExternalTaskID    string
	InputRequestID    string
	InputSchemaJSON   string
	State             string
	AnswerSHA256      string
	SubmissionID      string
}

func (t AgentInputSubmissionTarget) Valid() bool {
	if !t.WorkspaceID.IsValid() || !t.RunID.IsValid() || t.AttemptNo < 1 || t.AttemptNo > 100 || !validAgentInputID(t.ProviderID, 128, false) || !validAgentInputHandle(t.ProviderRequestID) || !validAgentInputHandle(t.ExternalTaskID) || !validAgentInputID(t.InputRequestID, 200, true) || validateAgentInputSchema(t.InputSchemaJSON) != nil {
		return false
	}
	switch t.State {
	case "pending":
		return t.AnswerSHA256 == "" && t.SubmissionID == ""
	case "sending", "unknown", "submitted":
		return len(t.AnswerSHA256) == 64 && validAgentInputID(t.SubmissionID, 200, true)
	default:
		return false
	}
}

type AgentInputSubmissionRecord struct {
	WorkspaceID     string
	RunID           string
	InputRequestID  string
	State           string
	Prompt          string
	InputSchemaJSON string
	RequestedAt     time.Time
	UpdatedAt       time.Time
}

type AgentInputClaim struct {
	Target AgentInputSubmissionTarget
	Record AgentInputSubmissionRecord
	Replay bool
}

type AgentInputSubmissionRepository interface {
	FindAgentInputPublic(context.Context, domain.WorkspaceID, domain.RunID) (AgentInputSubmissionRecord, error)
	FindAgentInputTarget(context.Context, domain.WorkspaceID, domain.RunID, string) (AgentInputSubmissionTarget, error)
	ClaimAgentInput(context.Context, AgentInputSubmissionTarget, PreparedAgentInput, time.Time) (AgentInputClaim, error)
	RecordAgentInputAccepted(context.Context, AgentInputSubmissionTarget, PreparedAgentInput, time.Time) (AgentInputSubmissionRecord, error)
	RecordAgentInputUnknown(context.Context, AgentInputSubmissionTarget, PreparedAgentInput, time.Time) (AgentInputSubmissionRecord, error)
}

type ProviderInputDisposition string

const (
	ProviderInputAccepted  ProviderInputDisposition = "accepted"
	ProviderInputUncertain ProviderInputDisposition = "unknown"
)

type ProviderInputResult struct {
	Disposition  ProviderInputDisposition
	SubmissionID string
}

type ProviderInputSource interface {
	SendProviderInput(context.Context, AgentInputSubmissionTarget, PreparedAgentInput) (ProviderInputResult, error)
}

type AgentInputSubmissions struct {
	authorizer ports.Authorizer
	repository AgentInputSubmissionRepository
	preparer   AgentInputPreparer
	source     ProviderInputSource
	clock      interface{ Now() time.Time }
}

func NewAgentInputSubmissions(authorizer ports.Authorizer, repository AgentInputSubmissionRepository, preparer AgentInputPreparer, source ProviderInputSource, clock interface{ Now() time.Time }) (*AgentInputSubmissions, error) {
	if authorizer == nil || repository == nil || preparer == nil || source == nil || clock == nil {
		return nil, ErrAgentInputUnavailable
	}
	return &AgentInputSubmissions{authorizer: authorizer, repository: repository, preparer: preparer, source: source, clock: clock}, nil
}

// ForAuthorizer reuses the same reviewed storage/transport runtime with a
// different authenticated principal projection. It never widens repository or
// provider capabilities; only the consumer-side authorization port changes.
func (s *AgentInputSubmissions) ForAuthorizer(authorizer ports.Authorizer) (*AgentInputSubmissions, error) {
	if s == nil || authorizer == nil || s.repository == nil || s.preparer == nil || s.source == nil || s.clock == nil {
		return nil, ErrAgentInputUnavailable
	}
	return &AgentInputSubmissions{authorizer: authorizer, repository: s.repository, preparer: s.preparer, source: s.source, clock: s.clock}, nil
}

func agentInputNow(clock interface{ Now() time.Time }) (time.Time, error) {
	at := clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return time.Time{}, ErrAgentInputUnavailable
	}
	return at, nil
}

func (s *AgentInputSubmissions) markUnknown(ctx context.Context, target AgentInputSubmissionTarget, prepared PreparedAgentInput) (AgentInputSubmissionRecord, error) {
	at, err := agentInputNow(s.clock)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	record, err := s.repository.RecordAgentInputUnknown(ctx, target, prepared, at)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	return record, ErrAgentInputOutcomeUnknown
}

func (s *AgentInputSubmissions) Submit(ctx context.Context, caller ports.Caller, runID domain.RunID, inputRequestID string, raw []byte) (AgentInputSubmissionRecord, error) {
	if err := ctx.Err(); err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	if !caller.WorkspaceID.IsValid() || !runID.IsValid() || caller.SubjectID == "" || caller.CredentialID == "" || !validAgentInputID(inputRequestID, 200, true) || len(raw) < 2 || len(raw) > 64<<10 {
		return AgentInputSubmissionRecord{}, ErrAgentInputInvalid
	}
	if err := s.authorizer.Authorize(ctx, caller, ports.InputRun, runID); err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	target, err := s.repository.FindAgentInputTarget(ctx, caller.WorkspaceID, runID, inputRequestID)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	if !target.Valid() || target.WorkspaceID != caller.WorkspaceID || target.RunID != runID || target.InputRequestID != inputRequestID {
		return AgentInputSubmissionRecord{}, ErrAgentInputConflict
	}
	prepared, err := s.preparer.PrepareAgentInput(target.InputSchemaJSON, inputRequestID, raw)
	if err != nil || !prepared.Valid() {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return AgentInputSubmissionRecord{}, err
		}
		return AgentInputSubmissionRecord{}, ErrAgentInputInvalid
	}
	at, err := agentInputNow(s.clock)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	claim, err := s.repository.ClaimAgentInput(ctx, target, prepared, at)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	if claim.Replay {
		return claim.Record, nil
	}
	if !claim.Target.Valid() || claim.Target.State != "sending" || claim.Target.WorkspaceID != caller.WorkspaceID || claim.Target.RunID != runID || claim.Target.InputRequestID != inputRequestID {
		return AgentInputSubmissionRecord{}, ErrAgentInputUnavailable
	}
	result, callErr := s.source.SendProviderInput(ctx, claim.Target, prepared)
	if ctx.Err() != nil {
		_, _ = s.markUnknown(context.WithoutCancel(ctx), claim.Target, prepared)
		return AgentInputSubmissionRecord{}, ctx.Err()
	}
	if callErr != nil || result.Disposition == ProviderInputUncertain {
		return s.markUnknown(ctx, claim.Target, prepared)
	}
	if result.Disposition != ProviderInputAccepted || result.SubmissionID != prepared.SubmissionID {
		if _, unknownErr := s.markUnknown(ctx, claim.Target, prepared); unknownErr != nil && !errors.Is(unknownErr, ErrAgentInputOutcomeUnknown) {
			return AgentInputSubmissionRecord{}, unknownErr
		}
		return AgentInputSubmissionRecord{}, ErrAgentInputUnavailable
	}
	at, err = agentInputNow(s.clock)
	if err != nil {
		return AgentInputSubmissionRecord{}, err
	}
	return s.repository.RecordAgentInputAccepted(ctx, claim.Target, prepared, at)
}
