package application

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"time"
)

var ErrAdmissionStorage = errors.New("admission storage unavailable")

type AdmissionRecord struct {
	WorkspaceID, SubjectID, CredentialID, IdempotencyKey, RequestHash, RunID, ReservationID string
	ToolVersionID, ToolsetVersionID, ConnectionID, PriceVersionID, DeploymentRevision       string
	BudgetID, PeriodID, Currency, CanonicalArguments                                        string
	ReservedMicro                                                                           int64
	CreatedAt                                                                               time.Time
}
type AdmissionRepository interface {
	FindReplay(context.Context, string, string, string) (AdmissionRecord, bool, error)
	InsertRun(context.Context, AdmissionRecord, domain.Run) error
	InsertJob(context.Context, AdmissionRecord) error
	InsertOutbox(context.Context, AdmissionRecord) error
}
type AdmissionService struct{ repo AdmissionRepository }

func NewAdmissionService(repo AdmissionRepository) (*AdmissionService, error) {
	if repo == nil {
		return nil, ErrAdmissionStorage
	}
	return &AdmissionService{repo}, nil
}
func (s *AdmissionService) FindReplay(ctx context.Context, w, subject, key string) (AdmissionRecord, bool, error) {
	return s.repo.FindReplay(ctx, w, subject, key)
}
func (s *AdmissionService) CreateRun(ctx context.Context, r AdmissionRecord) error {
	run, err := domain.NewQueuedRun(domain.RunID(r.RunID), domain.WorkspaceID(r.WorkspaceID), r.CreatedAt)
	if err != nil {
		return err
	}
	return s.repo.InsertRun(ctx, r, run)
}
func (s *AdmissionService) CreateJob(ctx context.Context, r AdmissionRecord) error {
	return s.repo.InsertJob(ctx, r)
}
func (s *AdmissionService) AppendOutbox(ctx context.Context, r AdmissionRecord) error {
	return s.repo.InsertOutbox(ctx, r)
}
