package httpexecutor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type Broker interface {
	Prepare(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error)
}

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type EgressPolicy struct {
	AllowedHosts  []string
	AllowHTTP     bool
	AllowLoopback bool
}

type Executor struct {
	broker                   Broker
	policy                   map[string]struct{}
	allowHTTP, allowLoopback bool
	resolver                 Resolver
	dialer                   Dialer
	clock                    Clock
}

func canonicalHost(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if value == "" || len(value) > 253 || strings.Contains(value, "%") {
		return "", supply.ErrExecutorForbidden
	}
	if net.ParseIP(value) != nil {
		return value, nil
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return "", supply.ErrExecutorForbidden
		}
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return "", supply.ErrExecutorForbidden
	}
	return value, nil
}

func New(broker Broker, policy EgressPolicy, resolver Resolver, dialer Dialer, clock Clock) (*Executor, error) {
	if broker == nil || len(policy.AllowedHosts) == 0 || len(policy.AllowedHosts) > 256 {
		return nil, supply.ErrExecutorUnavailable
	}
	hosts := make(map[string]struct{}, len(policy.AllowedHosts))
	for _, host := range policy.AllowedHosts {
		canonical, err := canonicalHost(host)
		if err != nil {
			return nil, supply.ErrExecutorUnavailable
		}
		if _, duplicate := hosts[canonical]; duplicate {
			return nil, supply.ErrExecutorUnavailable
		}
		hosts[canonical] = struct{}{}
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1}
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &Executor{broker: broker, policy: hosts, allowHTTP: policy.AllowHTTP, allowLoopback: policy.AllowLoopback, resolver: resolver, dialer: dialer, clock: clock}, nil
}

func validSubmission(s supply.Submission) bool {
	if s.WorkspaceID == "" || s.RunID == "" || s.AttemptNo == 0 || s.Generation == 0 || uint64(s.AttemptNo) != s.Generation || len(s.SubmissionKey) < 8 || len(s.SubmissionKey) > 200 {
		return false
	}
	for _, ch := range s.SubmissionKey {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func safeHeaderName(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}

func reservedHeader(value string) bool {
	switch strings.ToLower(value) {
	case "host", "content-length", "transfer-encoding", "connection", "upgrade", "proxy-connection", "te", "trailer":
		return true
	default:
		return false
	}
}

func safeHeaderValue(secret application.Secret) (string, bool) {
	value := secret.Bytes()
	if len(value) == 0 || len(value) > 16<<10 || !utf8.Valid(value) {
		return "", false
	}
	for _, ch := range value {
		if ch < 0x20 || ch > 0x7e {
			return "", false
		}
	}
	return string(value), true
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func addressAllowed(ip net.IP, allowLoopback bool) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsPrivate() {
		return false
	}
	if ip.IsLoopback() {
		return allowLoopback
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if cgnat.Contains(addr) {
		return false
	}
	return ip.IsGlobalUnicast()
}

func (e *Executor) resolveEndpoint(ctx context.Context, raw string) (*url.URL, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host == "" {
		return nil, "", supply.ErrExecutorForbidden
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && !(scheme == "http" && e.allowHTTP) {
		return nil, "", supply.ErrExecutorForbidden
	}
	host, err := canonicalHost(u.Hostname())
	if err != nil {
		return nil, "", err
	}
	if _, ok := e.policy[host]; !ok {
		return nil, "", supply.ErrExecutorForbidden
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else if value, parseErr := strconv.Atoi(port); parseErr != nil || value < 1 || value > 65535 {
		return nil, "", supply.ErrExecutorForbidden
	}
	var addresses []net.IP
	if literal := net.ParseIP(host); literal != nil {
		addresses = []net.IP{literal}
	} else {
		resolved, lookupErr := e.resolver.LookupIPAddr(ctx, host)
		if lookupErr != nil || len(resolved) == 0 || len(resolved) > 64 {
			return nil, "", supply.ErrExecutorUnavailable
		}
		for _, item := range resolved {
			addresses = append(addresses, item.IP)
		}
	}
	values := make([]string, 0, len(addresses))
	for _, ip := range addresses {
		if !addressAllowed(ip, e.allowLoopback) {
			return nil, "", supply.ErrExecutorForbidden
		}
		values = append(values, ip.String())
	}
	sort.Strings(values)
	if len(values) == 0 {
		return nil, "", supply.ErrExecutorUnavailable
	}
	return u, net.JoinHostPort(values[0], port), nil
}

func validPrepared(submission supply.Submission, prepared application.PreparedInvocation) bool {
	return prepared.WorkspaceID == submission.WorkspaceID && prepared.RunID == submission.RunID && prepared.Deployment.Validate() == nil && prepared.Deployment.State == "active" && prepared.Deployment.TransportKind == "http" && prepared.Deployment.HTTPMethod == "POST" && len(prepared.CanonicalArguments) > 0 && len(prepared.CanonicalArguments) <= prepared.Deployment.MaxRequestBytes
}

func buildHeaders(req *http.Request, deployment domain.Deployment, secret application.Secret, submissionKey string) error {
	if !safeHeaderName(deployment.IdempotencyHeader) || reservedHeader(deployment.IdempotencyHeader) {
		return supply.ErrExecutorForbidden
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(deployment.IdempotencyHeader, submissionKey)
	switch deployment.AuthMode {
	case "none":
		return nil
	case "bearer":
		value, ok := safeHeaderValue(secret)
		if !ok {
			return supply.ErrExecutorForbidden
		}
		req.Header.Set("Authorization", "Bearer "+value)
		return nil
	case "header":
		if !safeHeaderName(deployment.AuthHeaderName) || reservedHeader(deployment.AuthHeaderName) || strings.EqualFold(deployment.AuthHeaderName, deployment.IdempotencyHeader) {
			return supply.ErrExecutorForbidden
		}
		value, ok := safeHeaderValue(secret)
		if !ok {
			return supply.ErrExecutorForbidden
		}
		req.Header.Set(deployment.AuthHeaderName, value)
		return nil
	default:
		return supply.ErrExecutorForbidden
	}
}

func validProviderValue(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > 512 || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func decodeAccepted(body []byte) (supply.Result, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return supply.Result{}, errors.New("invalid provider response")
	}
	seen := map[string]bool{}
	var result supply.Result
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, ok := keyToken.(string)
		if tokenErr != nil || !ok || seen[key] {
			return supply.Result{}, errors.New("invalid provider response")
		}
		seen[key] = true
		var value string
		if err = decoder.Decode(&value); err != nil {
			return supply.Result{}, errors.New("invalid provider response")
		}
		switch key {
		case "provider_request_id":
			result.ProviderRequestID = value
		case "external_task_id":
			result.ExternalTaskID = value
		default:
			return supply.Result{}, errors.New("invalid provider response")
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return supply.Result{}, errors.New("invalid provider response")
	}
	if token, err = decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return supply.Result{}, errors.New("invalid provider response")
	}
	if !validProviderValue(result.ProviderRequestID, true) || !validProviderValue(result.ExternalTaskID, false) {
		return supply.Result{}, errors.New("invalid provider response")
	}
	result.Disposition = supply.Accepted
	return result, nil
}

func unknown() (supply.Result, error) { return supply.Result{Disposition: supply.Unknown}, nil }

func (e *Executor) Submit(ctx context.Context, submission supply.Submission) (supply.Result, error) {
	if err := ctx.Err(); err != nil {
		return supply.Result{}, err
	}
	if !validSubmission(submission) {
		return supply.Result{}, supply.ErrExecutorForbidden
	}
	at := e.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return supply.Result{}, supply.ErrExecutorUnavailable
	}
	prepared, err := e.broker.Prepare(ctx, application.InvocationRef{WorkspaceID: submission.WorkspaceID, RunID: submission.RunID}, at)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return supply.Result{}, err
		}
		if errors.Is(err, application.ErrInvocationForbidden) {
			return supply.Result{}, supply.ErrExecutorForbidden
		}
		return supply.Result{}, supply.ErrExecutorUnavailable
	}
	if !validPrepared(submission, prepared) {
		return supply.Result{}, supply.ErrExecutorUnavailable
	}
	requestCtx, cancel := context.WithTimeout(ctx, prepared.Deployment.RequestTimeout)
	defer cancel()
	endpoint, pinnedAddress, err := e.resolveEndpoint(requestCtx, prepared.Deployment.EndpointURL)
	if err != nil {
		if ctx.Err() != nil {
			return supply.Result{}, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return unknown()
		}
		return supply.Result{}, err
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint.String(), strings.NewReader(prepared.CanonicalArguments))
	if err != nil {
		return supply.Result{}, supply.ErrExecutorUnavailable
	}
	if err = buildHeaders(req, prepared.Deployment, prepared.Secret, submission.SubmissionKey); err != nil {
		return supply.Result{}, err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: prepared.Deployment.RequestTimeout,
		DialContext: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
			return e.dialer.DialContext(dialCtx, network, pinnedAddress)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return supply.Result{}, ctx.Err()
		}
		return unknown()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unknown()
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return unknown()
	}
	limit := int64(prepared.Deployment.MaxResponseBytes)
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return unknown()
	}
	result, err := decodeAccepted(body)
	if err != nil {
		return unknown()
	}
	result.ProviderID = prepared.Deployment.ProviderID
	return result, nil
}

var _ supply.Executor = (*Executor)(nil)
