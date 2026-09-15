package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidDeployment = errors.New("invalid supply deployment")

const (
	TransportHTTP              = "http"
	TransportMCPStreamableHTTP = "mcp_streamable_http"
	TransportAgentHTTP         = "agent_http"
)

type Deployment struct {
	Revision, ProviderID, TransportKind, EndpointURL, HTTPMethod string
	StatusEndpointURL, StatusHTTPMethod, CancelEndpointURL       string
	CancelHTTPMethod, InputEndpointURL, InputHTTPMethod          string
	AuthMode, AuthHeaderName, IdempotencyHeader                  string
	MCPProtocolVersion                                           string
	MCPStateless                                                 bool
	RequestTimeout                                               time.Duration
	MaxRequestBytes, MaxResponseBytes                            int
	State                                                        string
	CreatedAt                                                    time.Time
}

func reservedHTTPHeader(value string) bool {
	switch strings.ToLower(value) {
	case "host", "content-length", "content-type", "accept", "authorization", "transfer-encoding", "connection", "upgrade", "proxy-authorization", "proxy-connection", "te", "trailer":
		return true
	default:
		return false
	}
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
	if !validID(d.Revision) || !validID(d.ProviderID) || d.HTTPMethod != "POST" || len(d.EndpointURL) < 1 || len(d.EndpointURL) > 2048 || !utf8.ValidString(d.EndpointURL) || (!strings.HasPrefix(d.EndpointURL, "https://") && !strings.HasPrefix(d.EndpointURL, "http://")) || d.RequestTimeout < 100*time.Millisecond || d.RequestTimeout > 5*time.Minute || d.MaxRequestBytes < 1 || d.MaxRequestBytes > 1<<20 || d.MaxResponseBytes < 1 || d.MaxResponseBytes > 8<<20 || d.CreatedAt.IsZero() {
		return ErrInvalidDeployment
	}
	switch d.TransportKind {
	case TransportHTTP:
		if !validHeaderName(d.IdempotencyHeader) || reservedHTTPHeader(d.IdempotencyHeader) || d.MCPProtocolVersion != "" || d.MCPStateless {
			return ErrInvalidDeployment
		}
	case TransportAgentHTTP:
		if !validHeaderName(d.IdempotencyHeader) || reservedHTTPHeader(d.IdempotencyHeader) || d.MCPProtocolVersion != "" || d.MCPStateless ||
			d.StatusEndpointURL == "" || d.StatusHTTPMethod == "" || d.CancelEndpointURL == "" || d.CancelHTTPMethod == "" {
			return ErrInvalidDeployment
		}
	case TransportMCPStreamableHTTP:
		if d.IdempotencyHeader != "" || d.MCPProtocolVersion != "2026-07-28" || !d.MCPStateless || d.StatusEndpointURL != "" || d.StatusHTTPMethod != "" || d.CancelEndpointURL != "" || d.CancelHTTPMethod != "" || d.InputEndpointURL != "" || d.InputHTTPMethod != "" {
			return ErrInvalidDeployment
		}
	default:
		return ErrInvalidDeployment
	}
	if d.TransportKind != TransportAgentHTTP && (d.InputEndpointURL != "" || d.InputHTTPMethod != "") {
		return ErrInvalidDeployment
	}
	if !validOptionalControlEndpoint(d.StatusEndpointURL, d.StatusHTTPMethod) || !validOptionalControlEndpoint(d.CancelEndpointURL, d.CancelHTTPMethod) || !validOptionalControlEndpoint(d.InputEndpointURL, d.InputHTTPMethod) {
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
		if !validHeaderName(d.AuthHeaderName) || reservedHTTPHeader(d.AuthHeaderName) || strings.EqualFold(d.AuthHeaderName, d.IdempotencyHeader) {
			return ErrInvalidDeployment
		}
	default:
		return ErrInvalidDeployment
	}
	return nil
}

func validOptionalControlEndpoint(endpoint, method string) bool {
	if endpoint == "" || method == "" {
		return endpoint == "" && method == ""
	}
	if method != "POST" || len(endpoint) > 2048 || !utf8.ValidString(endpoint) || (!strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://")) {
		return false
	}
	if strings.Contains(endpoint, "#") {
		return false
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	hostEnd := len(rest)
	for _, separator := range []string{"/", "?"} {
		if at := strings.Index(rest, separator); at >= 0 && at < hostEnd {
			hostEnd = at
		}
	}
	host := rest[:hostEnd]
	return host != "" && !strings.Contains(host, "@")
}

func (d Deployment) SupportsStatusQuery() bool {
	return d.StatusEndpointURL != "" && d.StatusHTTPMethod == "POST"
}

func (d Deployment) SupportsCancellation() bool {
	return d.CancelEndpointURL != "" && d.CancelHTTPMethod == "POST"
}

func (d Deployment) SupportsSupplementalInput() bool {
	return d.Validate() == nil && d.TransportKind == TransportAgentHTTP && d.InputEndpointURL != "" && d.InputHTTPMethod == "POST"
}

func (d Deployment) SupportsMCPTools() bool {
	return d.Validate() == nil && d.TransportKind == TransportMCPStreamableHTTP && d.MCPProtocolVersion == "2026-07-28" && d.MCPStateless
}

func (d Deployment) SupportsRemoteAgent() bool {
	return d.Validate() == nil && d.TransportKind == TransportAgentHTTP && d.SupportsStatusQuery() && d.SupportsCancellation()
}
