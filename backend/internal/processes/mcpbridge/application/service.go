// Package application defines the MCP bridge use cases without depending on
// the MCP SDK, HTTP, Gin, PostgreSQL or any other transport/infrastructure.
package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid            = errors.New("invalid MCP bridge request")
	ErrUnauthenticated    = errors.New("MCP bridge requires authentication")
	ErrForbidden          = errors.New("MCP bridge forbidden")
	ErrNotFound           = errors.New("MCP bridge object not found")
	ErrConflict           = errors.New("MCP bridge conflict")
	ErrBudgetExceeded     = errors.New("MCP bridge budget exceeded")
	ErrOutcomeUnconfirmed = errors.New("MCP bridge outcome unconfirmed")
	ErrCommitUnconfirmed  = errors.New("MCP bridge commit unconfirmed")
	ErrUnavailable        = errors.New("MCP bridge dependency unavailable")
)

type Caller struct{ WorkspaceID, SubjectID, CredentialID string }

type Authenticator interface {
	Authenticate(context.Context, string) (Caller, error)
}

type StartRequest struct {
	IdempotencyKey, ToolID, ToolVersion, ToolsetVersionID, ConnectionID, Currency, MaxChargeMicro string
	Arguments                                                                                     []byte
}

type StartReceipt struct {
	WorkspaceID, RunID, ReservationID, Currency string
	ReservedMicro                               int64
	Replayed                                    bool
}

type Starter interface {
	Start(context.Context, Caller, StartRequest) (StartReceipt, error)
}

type Run struct {
	RunID, WorkspaceID, State string
	Version                   uint64
	CreatedAt, UpdatedAt      time.Time
}

type Artifact struct {
	ArtifactID, Kind, MediaType string
	SizeBytes                   int64
	CreatedAt                   time.Time
	ContentJSON                 string
}

type Runs interface {
	GetRun(context.Context, Caller, string) (Run, error)
	CancelRun(context.Context, Caller, string, string) (Run, error)
	GetArtifact(context.Context, Caller, string, string) (Artifact, error)
}

type Service struct {
	auth    Authenticator
	starter Starter
	runs    Runs
}

func New(auth Authenticator, starter Starter, runs Runs) (*Service, error) {
	if auth == nil || starter == nil || runs == nil {
		return nil, ErrUnavailable
	}
	return &Service{auth: auth, starter: starter, runs: runs}, nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (s *Service) Authenticate(ctx context.Context, token, workspace string) (Caller, error) {
	if s == nil || s.auth == nil || !validID(workspace) || strings.TrimSpace(token) == "" {
		return Caller{}, ErrInvalid
	}
	caller, err := s.auth.Authenticate(ctx, token)
	if err != nil {
		return Caller{}, err
	}
	if caller.WorkspaceID != workspace || !validID(caller.WorkspaceID) || !validID(caller.SubjectID) || !validID(caller.CredentialID) {
		return Caller{}, ErrForbidden
	}
	return caller, nil
}

func (s *Service) Start(ctx context.Context, caller Caller, request StartRequest) (StartReceipt, error) {
	if len(request.Arguments) < 2 || len(request.Arguments) > 65536 || !json.Valid(request.Arguments) || request.Arguments[0] != '{' {
		return StartReceipt{}, ErrInvalid
	}
	return s.starter.Start(ctx, caller, request)
}

func (s *Service) GetRun(ctx context.Context, caller Caller, runID string) (Run, error) {
	if !validID(runID) {
		return Run{}, ErrInvalid
	}
	return s.runs.GetRun(ctx, caller, runID)
}

func (s *Service) CancelRun(ctx context.Context, caller Caller, runID, reason string) (Run, error) {
	if !validID(runID) || len([]rune(reason)) > 500 || strings.ContainsRune(reason, 0) {
		return Run{}, ErrInvalid
	}
	return s.runs.CancelRun(ctx, caller, runID, reason)
}

func (s *Service) GetArtifact(ctx context.Context, caller Caller, runID, artifactID string) (Artifact, error) {
	if !validID(runID) || artifactID != "art_"+runID {
		return Artifact{}, ErrInvalid
	}
	return s.runs.GetArtifact(ctx, caller, runID, artifactID)
}
