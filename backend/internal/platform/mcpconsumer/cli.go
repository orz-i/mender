// Package mcpconsumer is an external consumer of Mender's public MCP surface.
// It imports no business context and never grants permissions or changes prices.
package mcpconsumer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxPayload = 1024 * 1024

var endpointPath = regexp.MustCompile(`^/mcp/v1/workspaces/[A-Za-z0-9_-]{1,128}(/toolsets/[A-Za-z0-9_-]{1,128})?$`)

type transport struct {
	base http.RoundTripper
	endpoint, key string
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Includes discovery GETs. Never attach the credential to another resource.
	if req.URL.String() != t.endpoint {
		return nil, errors.New("endpoint change rejected")
	}
	r := req.Clone(req.Context())
	r.Header = req.Header.Clone()
	r.Header.Set("Authorization", "Bearer "+t.key)
	resp, err := t.base.RoundTrip(r)
	if err != nil { return nil, errors.New("MCP transport unavailable") }
	if resp.ContentLength > maxPayload { _ = resp.Body.Close(); return nil, errors.New("MCP response too large") }
	resp.Body = &boundedBody{Reader: io.LimitReader(resp.Body, maxPayload+1), closer: resp.Body}
	return resp, nil
}

type boundedBody struct { io.Reader; closer io.Closer }
func (b *boundedBody) Close() error { return b.closer.Close() }

func endpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !endpointPath.MatchString(u.Path) {
		return nil, errors.New("invalid Mender endpoint")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil { loopback = ip.IsLoopback() }
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("HTTPS required except explicit loopback development endpoints")
	}
	return u, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil { return nil, errors.New("input file unavailable") }
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit { return nil, errors.New("invalid input file") }
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit { return nil, errors.New("input file exceeds limit") }
	return b, nil
}

// Snapshot contains only protocol tool contracts, not a new authorization source.
type Snapshot struct {
	Version int `json:"schema_version"`
	Protocol string `json:"protocol_version"`
	SHA256 string `json:"contract_sha256"`
	Tools []*mcp.Tool `json:"tools"`
}

func snapshot(tools []*mcp.Tool) (Snapshot, error) {
	for _, tool := range tools {
		if tool == nil { return Snapshot{}, errors.New("invalid discovery inventory") }
	}
	sort.Slice(tools, func(i,j int) bool { return tools[i].Name < tools[j].Name })
	for i,t := range tools { if t == nil || t.Name == "" || (i>0 && tools[i-1].Name == t.Name) { return Snapshot{}, errors.New("invalid discovery inventory") } }
	b, err := json.Marshal(tools)
	if err != nil || len(b)>maxPayload { return Snapshot{}, errors.New("discovery inventory exceeds limit") }
	h := sha256.Sum256(b)
	return Snapshot{Version:1,Protocol:"2026-07-28",SHA256:hex.EncodeToString(h[:]),Tools:tools},nil
}

// Run is testable without spawning a process. All flags precede the command.
// list/inspect/health never invoke a business tool. call requires --execute.
func Run(ctx context.Context, args []string, input io.Reader, output, diagnostic io.Writer) int {
	fail := func(code string) int { _, _ = fmt.Fprintln(diagnostic, code); return 1 }
	fs := flag.NewFlagSet("mender",flag.ContinueOnError)
	fs.SetOutput(io.Discard) // Unknown flags may contain an accidentally pasted secret.
	address := fs.String("endpoint","","explicit workspace or fixed Toolset MCP URL")
	keyFile := fs.String("key-file","","restricted machine credential file; never the credential itself")
	allowCall := fs.Bool("execute",false,"explicitly allow one tools/call; does not grant backend approval")
	query := fs.String("query","","local name/description filter on authorized discovery")
	baseline := fs.String("baseline","","read-only health comparison with prior discovery snapshot")
	duration := fs.Duration("timeout",15*time.Second,"request budget, maximum 2 minutes")
	if err := fs.Parse(args); err != nil { if errors.Is(err,flag.ErrHelp) { _, _ = fmt.Fprintln(output,"mender --endpoint URL --key-file FILE [--query TEXT | --baseline SNAPSHOT | --execute] list|inspect NAME|health|call NAME\ncall reads one JSON object from stdin; requests are never automatically retried. HTTPS required except loopback."); return 0 }; return fail("INVALID_COMMAND") }
	pos := fs.Args()
	if len(pos)==0 || *duration<=0 || *duration>2*time.Minute { return fail("INVALID_COMMAND") }
	command := pos[0]
	if !((command=="list" || command=="health") && len(pos)==1 || (command=="inspect" || command=="call") && len(pos)==2) { return fail("INVALID_COMMAND") }
	if command=="call" && !*allowCall { return fail("EXPLICIT_EXECUTE_REQUIRED") }
	if command!="health" && *baseline!="" || command!="list" && *query!="" { return fail("INVALID_COMMAND") }
	u, err := endpoint(*address)
	if err!=nil { return fail("INVALID_ENDPOINT") }
	keyBytes, err := readBounded(*keyFile,256)
	if err!=nil { return fail("CREDENTIAL_FILE_UNAVAILABLE") }
	if info,e := os.Stat(*keyFile); e!=nil || runtime.GOOS!="windows" && info.Mode().Perm()&0077!=0 { return fail("CREDENTIAL_FILE_PERMISSIONS") }
	key := strings.TrimSpace(string(keyBytes))
	if !regexp.MustCompile(`^[A-Za-z0-9._-]{8,240}$`).MatchString(key) { return fail("INVALID_CREDENTIAL_FILE") }
	var arguments json.RawMessage
	if command=="call" {
		b,e := io.ReadAll(io.LimitReader(input,64*1024+1))
		if e!=nil || len(b)>64*1024 || !json.Valid(b) || !bytes.HasPrefix(bytes.TrimSpace(b),[]byte("{")) { return fail("INVALID_ARGUMENTS") }
		arguments=b
	}
	ctx,cancel := context.WithTimeout(ctx,*duration); defer cancel()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy=nil // Do not disclose the key to an ambient HTTP proxy.
	defer base.CloseIdleConnections()
	hc := &http.Client{Transport:transport{base:base,endpoint:u.String(),key:key},Timeout:*duration,CheckRedirect:func(_ *http.Request,_ []*http.Request) error { return errors.New("redirect rejected") }}
	client := mcp.NewClient(&mcp.Implementation{Name:"mender-cli",Version:"0.1.0"},nil)
	session,err := client.Connect(ctx,&mcp.StreamableClientTransport{Endpoint:u.String(),HTTPClient:hc,DisableStandaloneSSE:true,MaxRetries:-1},nil)
	if err!=nil { return fail("MCP_CONNECTION_FAILED") }
	defer session.Close()
	if init:=session.InitializeResult(); init==nil || init.ProtocolVersion!="2026-07-28" { return fail("UNSUPPORTED_PROTOCOL") }
	var result any
	if command=="call" {
		r,e:=session.CallTool(ctx,&mcp.CallToolParams{Name:pos[1],Arguments:arguments})
		if e!=nil { return fail("CALL_OUTCOME_UNCONFIRMED_REUSE_ORIGINAL_IDEMPOTENCY_KEY") }
		if r.IsError { return fail("TOOL_REJECTED_OR_FAILED") }
		result=r
	} else {
		var tools []*mcp.Tool
		cursor:="";seen:=map[string]bool{}
		for page:=0; ;page++ {
			if page>=32 { return fail("DISCOVERY_PAGE_LIMIT") }
			r,e:=session.ListTools(ctx,&mcp.ListToolsParams{Cursor:cursor})
			if e!=nil { return fail("DISCOVERY_FAILED") }
			for _,t:=range r.Tools { if t==nil || t.Name=="" { return fail("INVALID_DISCOVERY") } }
			tools=append(tools,r.Tools...)
			if len(tools)>2000 { return fail("DISCOVERY_TOOL_LIMIT") }
			cursor=r.NextCursor
			if cursor=="" { break }
			if seen[cursor] { return fail("DISCOVERY_CURSOR_LOOP") }; seen[cursor]=true
		}
		s,e:=snapshot(tools);if e!=nil{return fail("INVALID_DISCOVERY")}
		switch command {
		case "inspect":
			for _,t:=range tools { if t.Name==pos[1] { result=t; break } }
			if result==nil { return fail("TOOL_NOT_VISIBLE") }
		case "list":
			if *query!="" { filtered:=make([]*mcp.Tool,0);for _,t:=range tools {if strings.Contains(strings.ToLower(t.Name+" "+t.Description),strings.ToLower(*query)){filtered=append(filtered,t)}};s,e=snapshot(filtered);if e!=nil{return fail("INVALID_DISCOVERY")}}
			result=s
		case "health":
			if *baseline!="" {
				b,e:=readBounded(*baseline,maxPayload);var prior Snapshot
				if e!=nil || json.Unmarshal(b,&prior)!=nil || prior.Version!=1 || prior.Protocol!=s.Protocol {return fail("INVALID_BASELINE")}
				for _,t:=range prior.Tools {if t==nil{return fail("INVALID_BASELINE")}}
				computed,e:=snapshot(prior.Tools)
				if e!=nil || computed.SHA256!=prior.SHA256 {return fail("INVALID_BASELINE")}
				if prior.SHA256!=s.SHA256{return fail("TOOL_CONTRACT_DRIFT_REVIEW_REQUIRED")}
			}
			result=map[string]any{"status":"reachable","contract_sha256":s.SHA256,"tool_count":len(tools),"business_tools_called":0,"baseline_compared":*baseline!=""}
		}
	}
	b,err := json.Marshal(result)
	if err!=nil || len(b)>maxPayload {return fail("OUTPUT_LIMIT_OR_ENCODING")}
	if bytes.Contains(b,[]byte(key)) {return fail("CREDENTIAL_REFLECTION_BLOCKED")}
	if _,err=fmt.Fprintln(output,string(b));err!=nil{return fail("OUTPUT_FAILED")}
	return 0
}
