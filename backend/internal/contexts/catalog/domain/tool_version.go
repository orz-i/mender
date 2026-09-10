package domain

import (
	"errors"
	"time"
)

var ErrInvalidToolVersion = errors.New("invalid tool version")

type ToolVersion struct {
	ID, ToolID, Version, ProviderID, PriceVersionID, DeploymentRevision string
	State                                                               string
	PublishedAt                                                         time.Time
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

func (v ToolVersion) Validate() error {
	if !validID(v.ID) || !validID(v.ToolID) || !validVersion(v.Version) || !validID(v.ProviderID) || !validID(v.PriceVersionID) || !validID(v.DeploymentRevision) || (v.State != "published" && v.State != "disabled") || v.PublishedAt.IsZero() {
		return ErrInvalidToolVersion
	}
	return nil
}

func (v ToolVersion) Callable() bool { return v.Validate() == nil && v.State == "published" }
