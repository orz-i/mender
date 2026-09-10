package domain

import (
	"errors"
	"time"
)

var ErrInvalidBinding = errors.New("invalid toolset binding")

type Binding struct {
	WorkspaceID, ToolsetVersionID, ToolID, ToolVersionLabel, ToolVersionID, BudgetID string
	State                                                                            string
	PublishedAt                                                                      time.Time
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, c := range value {
		if i == 0 && !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == ':' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func (b Binding) Callable() bool {
	return validID(b.WorkspaceID) && validID(b.ToolsetVersionID) && validID(b.ToolID) && validVersion(b.ToolVersionLabel) && validID(b.ToolVersionID) && validID(b.BudgetID) && b.State == "published" && !b.PublishedAt.IsZero()
}
