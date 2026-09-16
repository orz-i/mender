package domain

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidReleasePlan = errors.New("invalid release plan")
var ErrReleaseTransition = errors.New("release plan transition is not allowed")

type ReleaseState string

const (
	ReleaseDraft      ReleaseState = "draft"
	ReleaseCanary     ReleaseState = "canary"
	ReleaseActive     ReleaseState = "active"
	ReleaseDraining   ReleaseState = "draining"
	ReleaseRolledBack ReleaseState = "rolled_back"
	ReleaseDisabled   ReleaseState = "disabled"
)

const (
	MinCanaryObservation = time.Minute
	MaxCanaryObservation = 24 * time.Hour
)

type ReleasePlanSnapshot struct {
	WorkspaceID                 string
	ID                          string
	PluginID                    string
	PluginVersion               string
	ToolsetVersionID            string
	ToolVersionID               string
	ProviderID                  string
	StableDeploymentRevision    string
	CandidateDeploymentRevision string
	Revision                    int64
	State                       ReleaseState
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	CanaryStartedAt             time.Time
	ObservationUntil            time.Time
	ActivatedAt                 time.Time
	DrainingAt                  time.Time
	RolledBackAt                time.Time
	DisabledAt                  time.Time
}

type ReleasePlan struct{ snapshot ReleasePlanSnapshot }

func validReleaseID(value string) bool {
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

func validReleasePluginID(value string) bool {
	if len(value) < 3 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func validReleaseVersion(value string) bool {
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 20 || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	if !hasPrerelease {
		return true
	}
	if prerelease == "" || len(prerelease) > 64 {
		return false
	}
	for _, ch := range prerelease {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func validReleaseTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1 && value.Year() <= 9999
}

func validateReleaseSnapshot(s ReleasePlanSnapshot) error {
	for _, value := range []string{s.WorkspaceID, s.ID, s.ToolsetVersionID, s.ToolVersionID, s.ProviderID, s.StableDeploymentRevision, s.CandidateDeploymentRevision} {
		if !validReleaseID(value) {
			return ErrInvalidReleasePlan
		}
	}
	if !validReleasePluginID(s.PluginID) || !validReleaseVersion(s.PluginVersion) || s.StableDeploymentRevision == s.CandidateDeploymentRevision || s.Revision < 1 || !validReleaseTime(s.CreatedAt) || !validReleaseTime(s.UpdatedAt) || s.UpdatedAt.Before(s.CreatedAt) {
		return ErrInvalidReleasePlan
	}
	switch s.State {
	case ReleaseDraft:
		if !s.CanaryStartedAt.IsZero() || !s.ObservationUntil.IsZero() || !s.ActivatedAt.IsZero() || !s.DrainingAt.IsZero() || !s.RolledBackAt.IsZero() || !s.DisabledAt.IsZero() {
			return ErrInvalidReleasePlan
		}
	case ReleaseCanary:
		if !validReleaseTime(s.CanaryStartedAt) || !validReleaseTime(s.ObservationUntil) || !s.ObservationUntil.After(s.CanaryStartedAt) || s.ObservationUntil.Sub(s.CanaryStartedAt) < MinCanaryObservation || s.ObservationUntil.Sub(s.CanaryStartedAt) > MaxCanaryObservation || !s.ActivatedAt.IsZero() || !s.DrainingAt.IsZero() || !s.RolledBackAt.IsZero() || !s.DisabledAt.IsZero() {
			return ErrInvalidReleasePlan
		}
	case ReleaseActive:
		if !validReleaseTime(s.CanaryStartedAt) || !validReleaseTime(s.ObservationUntil) || !validReleaseTime(s.ActivatedAt) || s.ActivatedAt.Before(s.ObservationUntil) || !s.DrainingAt.IsZero() || !s.RolledBackAt.IsZero() || !s.DisabledAt.IsZero() {
			return ErrInvalidReleasePlan
		}
	case ReleaseDraining:
		if !validReleaseTime(s.CanaryStartedAt) || !validReleaseTime(s.ObservationUntil) || !validReleaseTime(s.DrainingAt) || s.DrainingAt.Before(s.CanaryStartedAt) || !s.RolledBackAt.IsZero() || !s.DisabledAt.IsZero() {
			return ErrInvalidReleasePlan
		}
	case ReleaseRolledBack:
		if !validReleaseTime(s.CanaryStartedAt) || !validReleaseTime(s.ObservationUntil) || !validReleaseTime(s.RolledBackAt) || s.RolledBackAt.Before(s.CanaryStartedAt) || !s.DisabledAt.IsZero() {
			return ErrInvalidReleasePlan
		}
	case ReleaseDisabled:
		if !validReleaseTime(s.DisabledAt) || s.DisabledAt.Before(s.CreatedAt) {
			return ErrInvalidReleasePlan
		}
	default:
		return ErrInvalidReleasePlan
	}
	return nil
}

func NewReleasePlan(snapshot ReleasePlanSnapshot) (ReleasePlan, error) {
	snapshot.State = ReleaseDraft
	snapshot.Revision = 1
	snapshot.CreatedAt = snapshot.CreatedAt.UTC().Truncate(time.Microsecond)
	snapshot.UpdatedAt = snapshot.CreatedAt
	if err := validateReleaseSnapshot(snapshot); err != nil {
		return ReleasePlan{}, err
	}
	return ReleasePlan{snapshot: snapshot}, nil
}

func RestoreReleasePlan(snapshot ReleasePlanSnapshot) (ReleasePlan, error) {
	if err := validateReleaseSnapshot(snapshot); err != nil {
		return ReleasePlan{}, err
	}
	return ReleasePlan{snapshot: snapshot}, nil
}

func (p ReleasePlan) Snapshot() ReleasePlanSnapshot { return p.snapshot }

func (p *ReleasePlan) transition(at time.Time) error {
	at = at.UTC().Truncate(time.Microsecond)
	if !validReleaseTime(at) || at.Before(p.snapshot.UpdatedAt) || p.snapshot.Revision == int64(^uint64(0)>>1) {
		return ErrReleaseTransition
	}
	p.snapshot.Revision++
	p.snapshot.UpdatedAt = at
	return nil
}

func (p *ReleasePlan) StartCanary(at time.Time, observation time.Duration) error {
	if p.snapshot.State != ReleaseDraft || observation < MinCanaryObservation || observation > MaxCanaryObservation {
		return ErrReleaseTransition
	}
	if err := p.transition(at); err != nil {
		return err
	}
	p.snapshot.State = ReleaseCanary
	p.snapshot.CanaryStartedAt = p.snapshot.UpdatedAt
	p.snapshot.ObservationUntil = p.snapshot.UpdatedAt.Add(observation)
	return validateReleaseSnapshot(p.snapshot)
}

func (p *ReleasePlan) Promote(at time.Time) error {
	if p.snapshot.State != ReleaseCanary || at.Before(p.snapshot.ObservationUntil) {
		return ErrReleaseTransition
	}
	if err := p.transition(at); err != nil {
		return err
	}
	p.snapshot.State = ReleaseActive
	p.snapshot.ActivatedAt = p.snapshot.UpdatedAt
	return validateReleaseSnapshot(p.snapshot)
}

func (p *ReleasePlan) Drain(at time.Time) error {
	if p.snapshot.State != ReleaseCanary && p.snapshot.State != ReleaseActive {
		return ErrReleaseTransition
	}
	if err := p.transition(at); err != nil {
		return err
	}
	p.snapshot.State = ReleaseDraining
	p.snapshot.DrainingAt = p.snapshot.UpdatedAt
	return validateReleaseSnapshot(p.snapshot)
}

func (p *ReleasePlan) Rollback(at time.Time) error {
	if p.snapshot.State != ReleaseCanary && p.snapshot.State != ReleaseActive && p.snapshot.State != ReleaseDraining {
		return ErrReleaseTransition
	}
	if err := p.transition(at); err != nil {
		return err
	}
	p.snapshot.State = ReleaseRolledBack
	p.snapshot.RolledBackAt = p.snapshot.UpdatedAt
	return validateReleaseSnapshot(p.snapshot)
}

func (p *ReleasePlan) EmergencyDisable(at time.Time) error {
	if p.snapshot.State == ReleaseDraft || p.snapshot.State == ReleaseDisabled {
		return ErrReleaseTransition
	}
	if err := p.transition(at); err != nil {
		return err
	}
	p.snapshot.State = ReleaseDisabled
	p.snapshot.DisabledAt = p.snapshot.UpdatedAt
	return validateReleaseSnapshot(p.snapshot)
}

func (p ReleasePlan) Route() (deployment string, blocked bool) {
	switch p.snapshot.State {
	case ReleaseCanary, ReleaseActive:
		return p.snapshot.CandidateDeploymentRevision, false
	case ReleaseDisabled:
		return "", true
	default:
		return p.snapshot.StableDeploymentRevision, false
	}
}
