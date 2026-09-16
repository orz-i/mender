package mcpconsumer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEndpointCredentialBoundary(t *testing.T) {
	for _,s:=range []string{"http://example.com/mcp/v1/workspaces/ws", "https://user:secret@example.com/mcp/v1/workspaces/ws", "https://example.com/mcp/v1/workspaces/ws?token=secret", "https://example.com/mcp/v1/workspaces/ws#fragment", "https://example.com/mcp/v1/workspaces/%77s", "file:///mcp/v1/workspaces/ws", "https://example.com/other"} {
		if _,e:=endpoint(s);e==nil{t.Errorf("accepted unsafe endpoint %q",s)}
	}
	for _,s:=range []string{"https://example.com/mcp/v1/workspaces/ws", "http://127.0.0.1:1234/mcp/v1/workspaces/ws/toolsets/set", "http://[::1]:1234/mcp/v1/workspaces/ws"} {if _,e:=endpoint(s);e!=nil{t.Fatal(e)}}
}

func TestOfficialSDKDiscoveryHealthAndExplicitCall(t *testing.T) {
	const token="test-only-machine-key"
	var calls atomic.Int32
	s:=mcp.NewServer(&mcp.Implementation{Name:"mender-cli-fixture",Version:"0.1.0"},nil)
	s.AddTool(&mcp.Tool{Name:"echo",Description:"read test fixture",InputSchema:map[string]any{"type":"object"}},func(_ context.Context,r *mcp.CallToolRequest)(*mcp.CallToolResult,error){calls.Add(1);return &mcp.CallToolResult{Content:[]mcp.Content{&mcp.TextContent{Text:string(r.Params.Arguments)}}},nil})
	h:=mcp.NewStreamableHTTPHandler(func(*http.Request)*mcp.Server{return s},&mcp.StreamableHTTPOptions{Stateless:true,JSONResponse:true})
	ts:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.Header.Get("Authorization")!="Bearer "+token{http.Error(w,"denied",401);return};h.ServeHTTP(w,r)}));defer ts.Close()
	key:=filepath.Join(t.TempDir(),"key");if err:=os.WriteFile(key,[]byte(token),0600);err!=nil{t.Fatal(err)}
	args:=[]string{"--endpoint",ts.URL+"/mcp/v1/workspaces/ws","--key-file",key}
	run:=func(extra []string,body string)(int,string,string){var out,diag bytes.Buffer;code:=Run(context.Background(),append(append([]string{},args...),extra...),strings.NewReader(body),&out,&diag);return code,out.String(),diag.String()}
	code,out,diag:=run([]string{"list"},"");if code!=0{t.Fatal(diag)}
	var inventory Snapshot;if json.Unmarshal([]byte(out),&inventory)!=nil || len(inventory.Tools)!=1 || calls.Load()!=0 {t.Fatal("discovery invoked tool or invalid inventory")}
	base:=filepath.Join(t.TempDir(),"snapshot.json");if err:=os.WriteFile(base,[]byte(out),0600);err!=nil{t.Fatal(err)}
	if c,o,d:=run([]string{"--baseline",base,"health"},"");c!=0 || !strings.Contains(o,`"business_tools_called":0`){t.Fatal(c,d)}
	if c,_,_:=run([]string{"call","echo"},`{}`);c==0 || calls.Load()!=0{t.Fatal("unconfirmed call executed")}
	if c,o,d:=run([]string{"--execute","call","echo"},`{"n":9007199254740993}`);c!=0 || !strings.Contains(o,"9007199254740993") || calls.Load()!=1{t.Fatal(c,o,d,calls.Load())}
	if c,_,_:=run([]string{"--execute","call","echo"},`[]`);c==0 || calls.Load()!=1{t.Fatal("invalid arguments executed")}
	if c,_,_:=run([]string{"inspect","hidden"},"");c==0{t.Fatal("unknown tool appeared")}
	if c,o,d:=run([]string{"--query","echo","list"},"");c!=0 || !strings.Contains(o,"echo"){t.Fatal(c,d)}
	s.AddTool(&mcp.Tool{Name:"new_tool",InputSchema:map[string]any{"type":"object"}},func(context.Context,*mcp.CallToolRequest)(*mcp.CallToolResult,error){calls.Add(1);return &mcp.CallToolResult{},nil})
	if c,_,d:=run([]string{"--baseline",base,"health"},"");c==0 || !strings.Contains(d,"DRIFT"){t.Fatal("drift silently accepted",c,d)}
	if calls.Load()!=1{t.Fatal("health probe executed a business tool")}
	if c,o,d:=run([]string{"--execute","call","echo"},`{"secret":"`+token+`"}`);c==0 || o!="" || strings.Contains(d,token){t.Fatal("reflected credential printed")}
}

func TestRedirectDoesNotLeakCredentialAndFailuresDoNotRetry(t *testing.T) {
	var targetCalls,sourceCalls atomic.Int32
	dest:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){targetCalls.Add(1);w.WriteHeader(200)}));defer dest.Close()
	src:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){sourceCalls.Add(1);http.Redirect(w,r,dest.URL+"/mcp/v1/workspaces/ws",307)}));defer src.Close()
	key:=filepath.Join(t.TempDir(),"key");if err:=os.WriteFile(key,[]byte("local-secret-only"),0600);err!=nil{t.Fatal(err)}
	var out,diag bytes.Buffer
	if Run(context.Background(),[]string{"--endpoint",src.URL+"/mcp/v1/workspaces/ws","--key-file",key,"list"},strings.NewReader(""),&out,&diag)==0{t.Fatal("redirect allowed")}
	if targetCalls.Load()!=0 || strings.Contains(diag.String(),"local-secret-only"){t.Fatal("credential leaked")}
	if sourceCalls.Load()>2{t.Fatal("unexpected retry loop",sourceCalls.Load())}
}
