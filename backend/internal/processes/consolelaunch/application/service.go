package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrUnauthenticated = errors.New("console launch authentication required")
	ErrForbidden       = errors.New("console launch forbidden")
	ErrUnavailable     = errors.New("console launch unavailable")
)

type Actor struct{ UserID string }

type Authorizer interface {
	Authenticate(context.Context, string) (Actor, error)
	Authorize(context.Context, Actor, string, string) error
}

type LaunchOption struct {
	WorkspaceID, ToolsetVersionID, ToolID, ToolVersion, ToolVersionID string
	Title, Description, InputSchema, SideEffect, Idempotency          string
	ConnectionID, ProviderID, Currency                                string
	ReserveMicro                                                      int64
}

type Repository interface {
	ListLaunchOptions(context.Context, string, string, time.Time) ([]LaunchOption, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	auth       Authorizer
	repository Repository
	clock      Clock
}

func New(auth Authorizer, repository Repository, clock Clock) (*Service, error) {
	if auth == nil || repository == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &Service{auth: auth, repository: repository, clock: clock}, nil
}

func validID(value string) bool {
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

func validVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, ch := range value {
		if i == 0 && !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return false
		}
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '+' || ch == '-') {
			return false
		}
	}
	return true
}

func validOption(item LaunchOption, workspace string) bool {
	if item.WorkspaceID != workspace || !validID(item.WorkspaceID) || !validID(item.ToolsetVersionID) || !validID(item.ToolID) || !validVersion(item.ToolVersion) || !validID(item.ToolVersionID) || !validID(item.ConnectionID) || !validID(item.ProviderID) || len(item.Currency) != 3 || item.ReserveMicro < 0 {
		return false
	}
	if item.Title == "" || len([]rune(item.Title)) > 200 || len([]rune(item.Description)) > 4000 || (item.SideEffect != "read_only" && item.SideEffect != "write") || (item.Idempotency != "safe_read" && item.Idempotency != "idempotent" && item.Idempotency != "unsafe") {
		return false
	}
	var schema map[string]any
	if len(item.InputSchema) < 2 || len(item.InputSchema) > 1<<20 || json.Unmarshal([]byte(item.InputSchema), &schema) != nil || schema == nil || schema["type"] != "object" {
		return false
	}
	for _, ch := range item.Currency {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Actor, error) {
	return s.auth.Authenticate(ctx, raw)
}

func (s *Service) List(ctx context.Context, actor Actor, workspace string) ([]LaunchOption, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(actor.UserID) || !validID(workspace) {
		return nil, ErrForbidden
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "workspace:read"); err != nil {
		return nil, err
	}
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return nil, ErrUnavailable
	}
	items, err := s.repository.ListLaunchOptions(ctx, workspace, actor.UserID, now)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !validOption(item, workspace) {
			return nil, ErrUnavailable
		}
	}
	return items, nil
}
