package public

import (
	"context"
	"time"
)

// AdmissionRecord is an internal integration contract, not a public HTTP response.
type AdmissionRecord struct {
	WorkspaceID, SubjectID, CredentialID, IdempotencyKey, RequestHash, RunID, ReservationID string
	ToolVersionID, ToolsetVersionID, ConnectionID, PriceVersionID, DeploymentRevision       string
	BudgetID, PeriodID, Currency, CanonicalArguments                                        string
	ReservedMicro                                                                           int64
	CreatedAt                                                                               time.Time
}
type Admission interface {
	FindReplay(context.Context, string, string, string) (AdmissionRecord, bool, error)
	CreateRun(context.Context, AdmissionRecord) error
	CreateJob(context.Context, AdmissionRecord) error
	AppendOutbox(context.Context, AdmissionRecord) error
}
