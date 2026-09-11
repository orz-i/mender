package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	admission "github.com/orz-i/mender/backend/internal/processes/admission/public"
)

type Admission struct{ service *application.Service }

func NewAdmission(service *application.Service) *Admission { return &Admission{service: service} }

func admissionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, application.ErrInvalid):
		return admission.ErrInvalid
	case errors.Is(err, application.ErrUnauthenticated):
		return admission.ErrUnauthenticated
	case errors.Is(err, application.ErrForbidden):
		return admission.ErrForbidden
	case errors.Is(err, application.ErrConflict):
		return admission.ErrConflict
	case errors.Is(err, application.ErrBudgetExceeded), errors.Is(err, application.ErrBudgetUnavailable):
		return admission.ErrBudgetExceeded
	case errors.Is(err, application.ErrCommitUnconfirmed):
		return admission.ErrCommitUnconfirmed
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return admission.ErrUnavailable
	}
}

func (f *Admission) Start(ctx context.Context, caller admission.Caller, request admission.Request) (admission.Receipt, error) {
	if f == nil || f.service == nil {
		return admission.Receipt{}, admission.ErrUnavailable
	}
	receipt, err := f.service.Admit(ctx, application.Caller{WorkspaceID: caller.WorkspaceID, SubjectID: caller.SubjectID, CredentialID: caller.CredentialID}, application.Request{
		IdempotencyKey: request.IdempotencyKey, ToolID: request.ToolID, ToolVersion: request.ToolVersion,
		ToolsetVersionID: request.ToolsetVersionID, ConnectionID: request.ConnectionID, Currency: request.Currency,
		MaxChargeMicro: request.MaxChargeMicro, Arguments: append([]byte(nil), request.Arguments...),
	})
	if err != nil {
		return admission.Receipt{}, admissionError(err)
	}
	return admission.Receipt{WorkspaceID: receipt.WorkspaceID, RunID: receipt.RunID, ReservationID: receipt.ReservationID, Currency: receipt.Currency, ReservedMicro: receipt.ReservedMicro, Replayed: receipt.Replayed}, nil
}

var _ admission.Admission = (*Admission)(nil)
