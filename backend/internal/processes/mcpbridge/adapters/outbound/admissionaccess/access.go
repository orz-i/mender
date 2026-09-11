package admissionaccess

import (
	"context"
	"errors"

	admission "github.com/orz-i/mender/backend/internal/processes/admission/public"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type Access struct{ admission admission.Admission }

func New(service admission.Admission) *Access { return &Access{admission: service} }

func admissionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, admission.ErrInvalid):
		return application.ErrInvalid
	case errors.Is(err, admission.ErrUnauthenticated):
		return application.ErrUnauthenticated
	case errors.Is(err, admission.ErrForbidden):
		return application.ErrForbidden
	case errors.Is(err, admission.ErrConflict):
		return application.ErrConflict
	case errors.Is(err, admission.ErrBudgetExceeded):
		return application.ErrBudgetExceeded
	case errors.Is(err, admission.ErrBudgetUnavailable):
		return application.ErrUnavailable
	case errors.Is(err, admission.ErrCommitUnconfirmed):
		return application.ErrCommitUnconfirmed
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrUnavailable
	}
}

func (a *Access) Start(ctx context.Context, c application.Caller, q application.StartRequest) (application.StartReceipt, error) {
	if a == nil || a.admission == nil {
		return application.StartReceipt{}, application.ErrUnavailable
	}
	r, err := a.admission.Start(ctx, admission.Caller{WorkspaceID: c.WorkspaceID, SubjectID: c.SubjectID, CredentialID: c.CredentialID}, admission.Request{
		IdempotencyKey: q.IdempotencyKey, ToolID: q.ToolID, ToolVersion: q.ToolVersion, ToolsetVersionID: q.ToolsetVersionID,
		ConnectionID: q.ConnectionID, Currency: q.Currency, MaxChargeMicro: q.MaxChargeMicro, Arguments: append([]byte(nil), q.Arguments...),
	})
	if err != nil {
		return application.StartReceipt{}, admissionError(err)
	}
	return application.StartReceipt{WorkspaceID: r.WorkspaceID, RunID: r.RunID, ReservationID: r.ReservationID, Currency: r.Currency, ReservedMicro: r.ReservedMicro, Replayed: r.Replayed}, nil
}

var _ application.Starter = (*Access)(nil)
