package supplyinput

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type Source struct {
	senders map[string]supply.ProviderInputSender
}

func New(senders map[string]supply.ProviderInputSender) (*Source, error) {
	if len(senders) < 1 || len(senders) > 256 {
		return nil, application.ErrAgentInputUnavailable
	}
	copyOf := make(map[string]supply.ProviderInputSender, len(senders))
	for providerID, sender := range senders {
		probe := application.AgentInputSubmissionTarget{
			WorkspaceID: "ws_probe", RunID: "run_probe", AttemptNo: 1, ProviderID: providerID,
			ProviderRequestID: "request/probe", ExternalTaskID: "task/probe", InputRequestID: "input.probe",
			InputSchemaJSON: `{"type":"object"}`, State: "pending",
		}
		if !probe.Valid() || sender == nil {
			return nil, application.ErrAgentInputUnavailable
		}
		copyOf[providerID] = sender
	}
	return &Source{senders: copyOf}, nil
}

func (s *Source) SendProviderInput(ctx context.Context, target application.AgentInputSubmissionTarget, prepared application.PreparedAgentInput) (application.ProviderInputResult, error) {
	if s == nil || !target.Valid() || !prepared.Valid() {
		return application.ProviderInputResult{}, application.ErrAgentInputInvalid
	}
	sender := s.senders[target.ProviderID]
	if sender == nil {
		return application.ProviderInputResult{}, application.ErrAgentInputUnavailable
	}
	result, err := sender.SendInput(ctx, supply.InputSubmission{
		WorkspaceID: string(target.WorkspaceID), RunID: string(target.RunID), AttemptNo: target.AttemptNo,
		ProviderID: target.ProviderID, ProviderRequestID: target.ProviderRequestID, ExternalTaskID: target.ExternalTaskID,
		InputRequestID: target.InputRequestID, SubmissionID: prepared.SubmissionID, AnswerJSON: prepared.AnswerJSON,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.ProviderInputResult{}, err
		}
		return application.ProviderInputResult{}, application.ErrAgentInputUnavailable
	}
	switch result.Disposition {
	case supply.InputAccepted:
		return application.ProviderInputResult{Disposition: application.ProviderInputAccepted, SubmissionID: result.SubmissionID}, nil
	case supply.InputUnknown:
		return application.ProviderInputResult{Disposition: application.ProviderInputUncertain, SubmissionID: result.SubmissionID}, nil
	default:
		return application.ProviderInputResult{}, application.ErrAgentInputUnavailable
	}
}

var _ application.ProviderInputSource = (*Source)(nil)
