package mcpclient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

const protocolVersion = "2026-07-28"

type Broker interface {
	Prepare(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error)
	PrepareMCPDiscovery(context.Context, application.MCPDiscoveryRef, time.Time) (application.PreparedMCPDiscovery, error)
}

type SnapshotRecorder interface {
	Record(context.Context, domain.MCPToolSnapshot) error
}
type RouteRepository interface {
	ResolveMCPToolRoute(context.Context, string, string) (domain.MCPToolRoute, error)
}
type ResultRepository interface {
	SaveMCPCallResult(context.Context, application.MCPCallResult) error
}
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
type Clock interface{ Now() time.Time }

type EgressPolicy struct {
	AllowedHosts  []string
	AllowHTTP     bool
	AllowLoopback bool
}

type Client struct {
	broker                   Broker
	routes                   RouteRepository
	snapshots                SnapshotRecorder
	results                  ResultRepository
	allowed                  map[string]struct{}
	allowHTTP, allowLoopback bool
	resolver                 Resolver
	dialer                   Dialer
	clock                    Clock
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

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

func New(broker Broker, routes RouteRepository, snapshots SnapshotRecorder, results ResultRepository, policy EgressPolicy, resolver Resolver, dialer Dialer, clock Clock) (*Client, error) {
	if broker == nil || routes == nil || snapshots == nil || results == nil || len(policy.AllowedHosts) == 0 || len(policy.AllowedHosts) > 256 {
		return nil, supply.ErrExecutorUnavailable
	}
	hosts := make(map[string]struct{}, len(policy.AllowedHosts))
	for _, host := range policy.AllowedHosts {
		value, err := canonicalHost(host)
		if err != nil {
			return nil, supply.ErrExecutorUnavailable
		}
		if _, exists := hosts[value]; exists {
			return nil, supply.ErrExecutorUnavailable
		}
		hosts[value] = struct{}{}
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
	return &Client{broker: broker, routes: routes, snapshots: snapshots, results: results, allowed: hosts, allowHTTP: policy.AllowHTTP, allowLoopback: policy.AllowLoopback, resolver: resolver, dialer: dialer, clock: clock}, nil
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

type endpoint struct {
	url    *url.URL
	pinned string
}

func (c *Client) resolve(ctx context.Context, deployment domain.Deployment) (endpoint, error) {
	u, err := url.Parse(deployment.EndpointURL)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host == "" || u.RawQuery != "" {
		return endpoint{}, supply.ErrExecutorForbidden
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && !(scheme == "http" && c.allowHTTP) {
		return endpoint{}, supply.ErrExecutorForbidden
	}
	host, err := canonicalHost(u.Hostname())
	if err != nil {
		return endpoint{}, err
	}
	if _, ok := c.allowed[host]; !ok {
		return endpoint{}, supply.ErrExecutorForbidden
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else if n, e := strconv.Atoi(port); e != nil || n < 1 || n > 65535 {
		return endpoint{}, supply.ErrExecutorForbidden
	}
	var addresses []net.IP
	if literal := net.ParseIP(host); literal != nil {
		addresses = []net.IP{literal}
	} else {
		resolved, e := c.resolver.LookupIPAddr(ctx, host)
		if e != nil || len(resolved) == 0 || len(resolved) > 64 {
			return endpoint{}, supply.ErrExecutorUnavailable
		}
		for _, item := range resolved {
			addresses = append(addresses, item.IP)
		}
	}
	for _, ip := range addresses {
		if !addressAllowed(ip, c.allowLoopback) {
			return endpoint{}, supply.ErrExecutorForbidden
		}
	}
	return endpoint{url: u, pinned: net.JoinHostPort(addresses[0].String(), port)}, nil
}

func secretHeader(secret application.Secret) (string, bool) {
	value := secret.Bytes()
	if len(value) == 0 || len(value) > 16<<10 || !utf8.Valid(value) {
		return "", false
	}
	for _, c := range value {
		if c < 0x20 || c > 0x7e {
			return "", false
		}
	}
	return string(value), true
}

type boundedBody struct {
	reader io.Reader
	closer io.Closer
}

func (b *boundedBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *boundedBody) Close() error               { return b.closer.Close() }

type authTransport struct {
	base       http.RoundTripper
	endpoint   string
	deployment domain.Deployment
	secret     application.Secret
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.String() != t.endpoint {
		return nil, supply.ErrExecutorForbidden
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Del("Authorization")
	clone.Header.Del("Proxy-Authorization")
	if version := clone.Header.Get("Mcp-Protocol-Version"); version != "" && version != protocolVersion {
		return nil, supply.ErrExecutorForbidden
	}
	if clone.ContentLength > int64(t.deployment.MaxRequestBytes) {
		return nil, supply.ErrExecutorForbidden
	}
	if t.deployment.AuthMode != "none" {
		value, ok := secretHeader(t.secret)
		if !ok {
			return nil, supply.ErrExecutorForbidden
		}
		if t.deployment.AuthMode == "bearer" {
			clone.Header.Set("Authorization", "Bearer "+value)
		} else {
			clone.Header.Set(t.deployment.AuthHeaderName, value)
		}
	}
	resp, err := t.base.RoundTrip(clone)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.ContentLength > int64(t.deployment.MaxResponseBytes) {
		_ = resp.Body.Close()
		return nil, supply.ErrExecutorUnavailable
	}
	resp.Body = &boundedBody{reader: io.LimitReader(resp.Body, int64(t.deployment.MaxResponseBytes)+1), closer: resp.Body}
	return resp, nil
}

func (c *Client) httpClient(deployment domain.Deployment, e endpoint, secret application.Secret) (*http.Client, func()) {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: deployment.RequestTimeout, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return c.dialer.DialContext(ctx, network, e.pinned)
	}}
	auth := &authTransport{base: transport, endpoint: e.url.String(), deployment: deployment, secret: secret}
	client := &http.Client{Transport: auth, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, transport.CloseIdleConnections
}

func (c *Client) connect(ctx context.Context, deployment domain.Deployment, secret application.Secret) (*mcp.ClientSession, func(), error) {
	if !deployment.SupportsMCPTools() || deployment.State != "active" {
		return nil, nil, supply.ErrExecutorForbidden
	}
	e, err := c.resolve(ctx, deployment)
	if err != nil {
		return nil, nil, err
	}
	httpClient, closeHTTP := c.httpClient(deployment, e, secret)
	client := mcp.NewClient(&mcp.Implementation{Name: "mender-upstream", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: e.url.String(), HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		closeHTTP()
		return nil, nil, err
	}
	closeAll := func() { _ = session.Close(); closeHTTP() }
	init := session.InitializeResult()
	if init == nil || init.ProtocolVersion != protocolVersion || init.Capabilities == nil || init.Capabilities.Tools == nil || session.ID() != "" {
		closeAll()
		return nil, nil, supply.ErrExecutorUnavailable
	}
	return session, closeAll, nil
}

type snapshotWire struct {
	Name, Title, Description   string
	Input, Output, Annotations json.RawMessage
}

func toolSnapshot(revision string, tool *mcp.Tool, at time.Time) (domain.MCPToolSnapshot, error) {
	if tool == nil {
		return domain.MCPToolSnapshot{}, supply.ErrExecutorUnavailable
	}
	input, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return domain.MCPToolSnapshot{}, supply.ErrExecutorUnavailable
	}
	output := []byte(nil)
	if tool.OutputSchema != nil {
		output, err = json.Marshal(tool.OutputSchema)
		if err != nil {
			return domain.MCPToolSnapshot{}, supply.ErrExecutorUnavailable
		}
	}
	annotations := []byte(`{}`)
	if tool.Annotations != nil {
		annotations, err = json.Marshal(tool.Annotations)
		if err != nil {
			return domain.MCPToolSnapshot{}, supply.ErrExecutorUnavailable
		}
	}
	title := tool.Title
	if title == "" && tool.Annotations != nil {
		title = tool.Annotations.Title
	}
	if title == "" {
		title = tool.Name
	}
	w := snapshotWire{Name: tool.Name, Title: title, Description: tool.Description, Input: input, Output: output, Annotations: annotations}
	encoded, err := json.Marshal(w)
	if err != nil {
		return domain.MCPToolSnapshot{}, supply.ErrExecutorUnavailable
	}
	digest := sha256.Sum256(encoded)
	s := domain.MCPToolSnapshot{DeploymentRevision: revision, ToolName: tool.Name, Title: title, Description: tool.Description, InputSchema: string(input), OutputSchema: string(output), AnnotationsJSON: string(annotations), ContentSHA256: hex.EncodeToString(digest[:]), DiscoveredAt: at}
	if s.Validate() != nil {
		return domain.MCPToolSnapshot{}, supply.ErrExecutorUnavailable
	}
	return s, nil
}

func (c *Client) list(ctx context.Context, session *mcp.ClientSession, revision string, at time.Time) ([]domain.MCPToolSnapshot, error) {
	cursor := ""
	seenCursor := map[string]bool{}
	seenTool := map[string]bool{}
	result := []domain.MCPToolSnapshot{}
	for page := 0; page < 20; page++ {
		listed, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, tool := range listed.Tools {
			s, err := toolSnapshot(revision, tool, at)
			if err != nil {
				return nil, err
			}
			if seenTool[s.ToolName] {
				return nil, supply.ErrExecutorUnavailable
			}
			seenTool[s.ToolName] = true
			result = append(result, s)
			if len(result) > 1000 {
				return nil, supply.ErrExecutorUnavailable
			}
		}
		if listed.NextCursor == "" {
			return result, nil
		}
		if seenCursor[listed.NextCursor] {
			return nil, supply.ErrExecutorUnavailable
		}
		seenCursor[listed.NextCursor] = true
		cursor = listed.NextCursor
	}
	return nil, supply.ErrExecutorUnavailable
}

func (c *Client) Discover(ctx context.Context, ref application.MCPDiscoveryRef) ([]domain.MCPToolSnapshot, error) {
	at := c.clock.Now().UTC().Truncate(time.Microsecond)
	prepared, err := c.broker.PrepareMCPDiscovery(ctx, ref, at)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, prepared.Deployment.RequestTimeout)
	defer cancel()
	session, closeIt, err := c.connect(requestCtx, prepared.Deployment, prepared.Secret)
	if err != nil {
		return nil, err
	}
	defer closeIt()
	items, err := c.list(requestCtx, session, prepared.Deployment.Revision, at)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err = c.snapshots.Record(ctx, item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func findSnapshot(items []domain.MCPToolSnapshot, route domain.MCPToolRoute) bool {
	for _, item := range items {
		if item.ToolName == route.UpstreamToolName && item.ContentSHA256 == route.SnapshotSHA256 {
			return true
		}
	}
	return false
}

func normalizeResult(result *mcp.CallToolResult) (string, string, bool) {
	if result == nil || result.NeedsInput() {
		return "", "", false
	}
	if result.IsError {
		return "", "MCP_TOOL_ERROR", true
	}
	var value any
	if result.StructuredContent != nil {
		value = result.StructuredContent
	} else {
		value = map[string]any{"content": result.Content}
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) == 0 || len(encoded) > 1<<20 || !json.Valid(encoded) {
		return "", "", false
	}
	return string(encoded), "", true
}

func (c *Client) persistKnown(ctx context.Context, prepared application.PreparedInvocation, submission supply.Submission, resultJSON, errorCode string) (supply.Result, error) {
	at := c.clock.Now().UTC().Truncate(time.Microsecond)
	state := "succeeded"
	if errorCode != "" {
		state = "failed"
	}
	value := application.MCPCallResult{WorkspaceID: submission.WorkspaceID, RunID: submission.RunID, DeploymentRevision: prepared.Deployment.Revision, SubmissionKey: submission.SubmissionKey, ProviderID: prepared.Deployment.ProviderID, ProviderRequestID: submission.SubmissionKey, State: state, ResultJSON: resultJSON, ErrorCode: errorCode, ObservedAt: at}
	if value.Validate() != nil || c.results.SaveMCPCallResult(ctx, value) != nil {
		return supply.Result{Disposition: supply.Unknown, ProviderID: prepared.Deployment.ProviderID, ProviderRequestID: submission.SubmissionKey}, nil
	}
	return supply.Result{Disposition: supply.Accepted, ProviderID: prepared.Deployment.ProviderID, ProviderRequestID: submission.SubmissionKey}, nil
}

func (c *Client) Submit(ctx context.Context, submission supply.Submission) (supply.Result, error) {
	at := c.clock.Now().UTC().Truncate(time.Microsecond)
	prepared, err := c.broker.Prepare(ctx, application.InvocationRef{WorkspaceID: submission.WorkspaceID, RunID: submission.RunID}, at)
	if err != nil {
		return supply.Result{}, err
	}
	if !prepared.Deployment.SupportsMCPTools() {
		return supply.Result{}, supply.ErrExecutorForbidden
	}
	route, err := c.routes.ResolveMCPToolRoute(ctx, prepared.ToolVersionID, prepared.Deployment.Revision)
	if err != nil {
		return c.persistKnown(ctx, prepared, submission, "", "MCP_ROUTE_UNAVAILABLE")
	}
	requestCtx, cancel := context.WithTimeout(ctx, prepared.Deployment.RequestTimeout)
	defer cancel()
	session, closeIt, err := c.connect(requestCtx, prepared.Deployment, prepared.Secret)
	if err != nil {
		return c.persistKnown(ctx, prepared, submission, "", "MCP_UPSTREAM_UNAVAILABLE")
	}
	defer closeIt()
	items, err := c.list(requestCtx, session, prepared.Deployment.Revision, at)
	if err != nil {
		return c.persistKnown(ctx, prepared, submission, "", "MCP_DISCOVERY_UNAVAILABLE")
	}
	if !findSnapshot(items, route) {
		return c.persistKnown(ctx, prepared, submission, "", "MCP_SCHEMA_DRIFT")
	}
	result, err := session.CallTool(requestCtx, &mcp.CallToolParams{Name: route.UpstreamToolName, Arguments: json.RawMessage(prepared.CanonicalArguments)})
	if err != nil || requestCtx.Err() != nil {
		if ctx.Err() != nil {
			return supply.Result{}, ctx.Err()
		}
		return supply.Result{Disposition: supply.Unknown, ProviderID: prepared.Deployment.ProviderID, ProviderRequestID: submission.SubmissionKey}, nil
	}
	resultJSON, errorCode, known := normalizeResult(result)
	if !known {
		return supply.Result{Disposition: supply.Unknown, ProviderID: prepared.Deployment.ProviderID, ProviderRequestID: submission.SubmissionKey}, nil
	}
	return c.persistKnown(ctx, prepared, submission, resultJSON, errorCode)
}

var _ supply.Executor = (*Client)(nil)
