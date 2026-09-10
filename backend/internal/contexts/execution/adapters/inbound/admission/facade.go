package admission

import (
	"context"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	contract "github.com/orz-i/mender/backend/internal/contexts/execution/public"
)

type Facade struct{ service *application.AdmissionService }

func New(s *application.AdmissionService) *Facade { return &Facade{s} }
func (f *Facade) FindReplay(ctx context.Context, w, s, k string) (contract.AdmissionRecord, bool, error) {
	r, found, e := f.service.FindReplay(ctx, w, s, k)
	return contract.AdmissionRecord(r), found, e
}
func (f *Facade) CreateRun(ctx context.Context, r contract.AdmissionRecord) error {
	return f.service.CreateRun(ctx, application.AdmissionRecord(r))
}
func (f *Facade) CreateJob(ctx context.Context, r contract.AdmissionRecord) error {
	return f.service.CreateJob(ctx, application.AdmissionRecord(r))
}
func (f *Facade) AppendOutbox(ctx context.Context, r contract.AdmissionRecord) error {
	return f.service.AppendOutbox(ctx, application.AdmissionRecord(r))
}

var _ contract.Admission = (*Facade)(nil)
