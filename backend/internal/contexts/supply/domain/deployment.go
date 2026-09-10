package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidDeployment = errors.New("invalid supply deployment")

type Deployment struct {
	Revision, ProviderID, TransportKind, EndpointURL, HTTPMethod string
	AuthMode, AuthHeaderName, IdempotencyHeader                  string
	RequestTimeout                                               time.Duration
	MaxRequestBytes, MaxResponseBytes                            int
	State                                                        string
	CreatedAt                                                    time.Time
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

func validHeaderName(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func (d Deployment) Validate() error {
	if !validID(d.Revision) || !validID(d.ProviderID) || d.TransportKind != "http" || d.HTTPMethod != "POST" || len(d.EndpointURL) < 1 || len(d.EndpointURL) > 2048 || !utf8.ValidString(d.EndpointURL) || (!strings.HasPrefix(d.EndpointURL, "https://") && !strings.HasPrefix(d.EndpointURL, "http://")) || !validHeaderName(d.IdempotencyHeader) || d.RequestTimeout < 100*time.Millisecond || d.RequestTimeout > 5*time.Minute || d.MaxRequestBytes < 1 || d.MaxRequestBytes > 1<<20 || d.MaxResponseBytes < 1 || d.MaxResponseBytes > 8<<20 || d.CreatedAt.IsZero() {
		return ErrInvalidDeployment
	}
	switch d.State {
	case "active", "disabled":
	default:
		return ErrInvalidDeployment
	}
	switch d.AuthMode {
	case "none", "bearer":
		if d.AuthHeaderName != "" {
			return ErrInvalidDeployment
		}
	case "header":
		if !validHeaderName(d.AuthHeaderName) {
			return ErrInvalidDeployment
		}
	default:
		return ErrInvalidDeployment
	}
	return nil
}
