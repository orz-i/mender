package application

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrUnsafeCancellation = errors.New("run is not a provably unexecuted admission")
var ErrCancellationStorage = errors.New("cancellation storage unavailable")

type CancellationRef struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
}
type CancellationChange struct {
	SubjectID, CredentialID, Reason string
	At                              time.Time
}
type CancellationState struct {
	WorkspaceID, RunID, State string
	Version                   uint64
	CreatedAt, UpdatedAt      time.Time
	Replayed                  bool
}
type CancellationData struct {
	Run                                              domain.Run
	Job                                              domain.Job
	AdmissionDelivery                                string
	HasAttempts                                      bool
	HasReceipt                                       bool
	ReceiptRef                                       CancellationRef
	ReceiptVersion                                   uint64
	ReceiptAt                                        time.Time
	ReceiptSubject, ReceiptCredential, ReceiptReason string
	CanceledEventMatches                             bool
}
type CancellationRepository interface {
	FindReference(context.Context, string, string) (CancellationRef, bool, error)
	LoadCancellation(context.Context, CancellationRef) (CancellationData, error)
	SaveCancellation(context.Context, CancellationRef, domain.Run, uint64, domain.JobSnapshot, domain.Job, CancellationChange) error
}
type CancellationService struct{ repo CancellationRepository }

func NewCancellationService(repo CancellationRepository) (*CancellationService, error) {
	if repo == nil {
		return nil, ErrCancellationStorage
	}
	return &CancellationService{repo}, nil
}
func (s *CancellationService) FindReference(ctx context.Context, w, id string) (CancellationRef, bool, error) {
	return s.repo.FindReference(ctx, w, id)
}
func cancellationView(d CancellationData) CancellationState {
	v := d.Run.Snapshot()
	return CancellationState{string(v.WorkspaceID), string(v.ID), string(v.State), v.Version, v.CreatedAt, v.UpdatedAt, d.HasReceipt}
}
func (s *CancellationService) load(ctx context.Context, ref CancellationRef) (CancellationData, error) {
	d, e := s.repo.LoadCancellation(ctx, ref)
	if e != nil {
		return d, e
	}
	v := d.Run.Snapshot()
	j := d.Job.Snapshot()
	if v.ID != domain.RunID(ref.RunID) || v.WorkspaceID != domain.WorkspaceID(ref.WorkspaceID) || j.RunID != v.ID || j.WorkspaceID != v.WorkspaceID || !j.CreatedAt.Equal(v.CreatedAt) || j.AttemptCount != 0 || j.LeaseGeneration != 0 || d.HasAttempts {
		return d, ErrUnsafeCancellation
	}
	if d.HasReceipt {
		if d.ReceiptRef != ref || v.State != domain.Canceled || v.Version != d.ReceiptVersion || v.Version != 2 || !j.StoppedAt.Equal(v.UpdatedAt) || !d.ReceiptAt.Equal(v.UpdatedAt) || d.AdmissionDelivery != "suppressed" || j.State != domain.JobCanceled || !d.CanceledEventMatches {
			return d, ErrUnsafeCancellation
		}
	} else {
		safeJob := j.State == domain.JobBlocked && j.BlockedReason == domain.BlockExecutorNotConfigured || j.State == domain.JobQueued && j.BlockedReason == ""
		if v.State != domain.Queued || v.Version != 1 || !safeJob || !j.StoppedAt.IsZero() || d.AdmissionDelivery != "pending" || d.CanceledEventMatches {
			return d, ErrUnsafeCancellation
		}
	}
	return d, nil
}
func (s *CancellationService) Inspect(ctx context.Context, ref CancellationRef) (CancellationState, error) {
	d, e := s.load(ctx, ref)
	if e != nil {
		return CancellationState{}, e
	}
	return cancellationView(d), nil
}
func (s *CancellationService) Cancel(ctx context.Context, ref CancellationRef, ch CancellationChange) (CancellationState, error) {
	if ch.SubjectID == "" || ch.CredentialID == "" || !utf8.ValidString(ch.Reason) || strings.ContainsRune(ch.Reason, 0) || len([]rune(ch.Reason)) > 500 {
		return CancellationState{}, ErrInvalidRequest
	}
	d, e := s.load(ctx, ref)
	if e != nil {
		return CancellationState{}, e
	}
	if d.HasReceipt {
		return CancellationState{}, ErrUnsafeCancellation
	}
	v := d.Run.Snapshot()
	beforeJob := d.Job.Snapshot()
	if ch.At.IsZero() || ch.At.Year() > 9999 {
		return CancellationState{}, ErrUnsafeCancellation
	}
	changed, e := d.Run.RequestCancel(ch.At)
	if e != nil || !changed || d.Run.Snapshot().State != domain.Canceled {
		return CancellationState{}, ErrUnsafeCancellation
	}
	if e = d.Job.CancelNeverLeased(ch.At); e != nil {
		return CancellationState{}, ErrUnsafeCancellation
	}
	if e = s.repo.SaveCancellation(ctx, ref, d.Run, v.Version, beforeJob, d.Job, ch); e != nil {
		return CancellationState{}, e
	}
	d.HasReceipt = false
	return cancellationView(d), nil
}
