package application

import (
	"context"
	"time"
)

const providerCallbackInboxMaxPage = 100

type ProviderCallbackInboxFilter struct {
	ProviderID       string
	Disposition      string
	ReasonCode       string
	BeforeReceivedAt time.Time
	BeforeReceiptID  string
	Limit            int
}

type ProviderCallbackInboxItem struct {
	ReceiptID, WorkspaceID, ProviderID, EventID, RunID, ObservationID string
	ObservationState, Disposition, ReasonCode                         string
	DeliveryCount, DuplicateDeliveryCount                             int
	ReceivedAt, LastReceivedAt, ObservedAt, ProcessedAt               time.Time
}

type ProviderCallbackInboxPage struct {
	Items                []ProviderCallbackInboxItem
	NextBeforeReceivedAt time.Time
	NextBeforeReceiptID  string
}

type ProviderCallbackInboxRepository interface {
	ListProviderCallbackInbox(context.Context, string, ProviderCallbackInboxFilter) (ProviderCallbackInboxPage, error)
}

type ProviderCallbackInboxService struct {
	repository ProviderCallbackInboxRepository
	auth       Authorizer
}

func NewProviderCallbackInbox(repository ProviderCallbackInboxRepository, auth Authorizer) (*ProviderCallbackInboxService, error) {
	if repository == nil || auth == nil {
		return nil, ErrUnavailable
	}
	return &ProviderCallbackInboxService{repository: repository, auth: auth}, nil
}

func validCallbackDisposition(value string) bool {
	return value == "pending" || value == "accepted" || value == "quarantined"
}

func validCallbackReason(value string) bool {
	switch value {
	case "event_id_conflict", "attempt_binding_mismatch", "observation_before_submission", "out_of_order", "terminal_replay", "execution_conflict":
		return true
	default:
		return false
	}
}

func validCallbackReceiptID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func validCallbackEventID(value string) bool {
	if len(value) < 1 || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func validProviderCallbackInboxFilter(filter ProviderCallbackInboxFilter) bool {
	if filter.ProviderID != "" && !validID(filter.ProviderID) || filter.Disposition != "" && !validCallbackDisposition(filter.Disposition) ||
		filter.ReasonCode != "" && !validCallbackReason(filter.ReasonCode) || filter.Limit < 1 || filter.Limit > providerCallbackInboxMaxPage {
		return false
	}
	if filter.BeforeReceivedAt.IsZero() != (filter.BeforeReceiptID == "") {
		return false
	}
	return filter.BeforeReceiptID == "" || validCallbackReceiptID(filter.BeforeReceiptID)
}

func validProviderCallbackInboxItem(item ProviderCallbackInboxItem, workspace string) bool {
	if item.WorkspaceID != workspace || !validCallbackReceiptID(item.ReceiptID) || !validID(item.ProviderID) || !validCallbackEventID(item.EventID) ||
		!validID(item.RunID) || !validCallbackEventID(item.ObservationID) || !validCallbackDisposition(item.Disposition) ||
		item.DeliveryCount < 1 || item.DeliveryCount > 1_000_000 || item.DuplicateDeliveryCount != item.DeliveryCount-1 ||
		item.ReceivedAt.IsZero() || item.LastReceivedAt.Before(item.ReceivedAt) || item.ObservedAt.IsZero() {
		return false
	}
	switch item.ObservationState {
	case "pending", "succeeded", "failed", "canceled":
	default:
		return false
	}
	if item.Disposition == "quarantined" {
		return validCallbackReason(item.ReasonCode) && !item.ProcessedAt.IsZero()
	}
	if item.ReasonCode != "" {
		return false
	}
	if item.Disposition == "accepted" {
		return !item.ProcessedAt.IsZero()
	}
	return item.ProcessedAt.IsZero()
}

func (s *ProviderCallbackInboxService) List(ctx context.Context, actor Actor, workspace string, filter ProviderCallbackInboxFilter) (ProviderCallbackInboxPage, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return ProviderCallbackInboxPage{}, ErrForbidden
	}
	if !validProviderCallbackInboxFilter(filter) {
		return ProviderCallbackInboxPage{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "execution:governance"); err != nil {
		return ProviderCallbackInboxPage{}, err
	}
	page, err := s.repository.ListProviderCallbackInbox(ctx, workspace, filter)
	if err != nil {
		return ProviderCallbackInboxPage{}, err
	}
	if len(page.Items) > filter.Limit || page.NextBeforeReceivedAt.IsZero() != (page.NextBeforeReceiptID == "") {
		return ProviderCallbackInboxPage{}, ErrUnavailable
	}
	var previousAt time.Time
	var previousID string
	for i, item := range page.Items {
		if !validProviderCallbackInboxItem(item, workspace) {
			return ProviderCallbackInboxPage{}, ErrUnavailable
		}
		if filter.ProviderID != "" && item.ProviderID != filter.ProviderID || filter.Disposition != "" && item.Disposition != filter.Disposition || filter.ReasonCode != "" && item.ReasonCode != filter.ReasonCode {
			return ProviderCallbackInboxPage{}, ErrUnavailable
		}
		if !filter.BeforeReceivedAt.IsZero() && !(item.ReceivedAt.Before(filter.BeforeReceivedAt) || item.ReceivedAt.Equal(filter.BeforeReceivedAt) && item.ReceiptID < filter.BeforeReceiptID) {
			return ProviderCallbackInboxPage{}, ErrUnavailable
		}
		if i > 0 && !(item.ReceivedAt.Before(previousAt) || item.ReceivedAt.Equal(previousAt) && item.ReceiptID < previousID) {
			return ProviderCallbackInboxPage{}, ErrUnavailable
		}
		previousAt, previousID = item.ReceivedAt, item.ReceiptID
	}
	if !page.NextBeforeReceivedAt.IsZero() && (len(page.Items) == 0 || !page.NextBeforeReceivedAt.Equal(page.Items[len(page.Items)-1].ReceivedAt) || page.NextBeforeReceiptID != page.Items[len(page.Items)-1].ReceiptID) {
		return ProviderCallbackInboxPage{}, ErrUnavailable
	}
	return page, nil
}
