package supply_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	httpexecutor "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/http"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type httpBrokerFunc func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error)

func (f httpBrokerFunc) Prepare(ctx context.Context, ref application.InvocationRef, at time.Time) (application.PreparedInvocation, error) {
	return f(ctx, ref, at)
}

type httpResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f httpResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

type httpDialerFunc func(context.Context, string, string) (net.Conn, error)

func (f httpDialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

type httpClock struct{ at time.Time }

func (c httpClock) Now() time.Time { return c.at }

func httpPrepared(t *testing.T, endpoint, auth string, maxResponse int, timeout time.Duration) application.PreparedInvocation {
	t.Helper()
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d := domain.Deployment{
		Revision: "deploy_http", ProviderID: "provider_http", TransportKind: "http",
		EndpointURL: endpoint, HTTPMethod: "POST", AuthMode: auth, IdempotencyHeader: "Idempotency-Key",
		RequestTimeout: timeout, MaxRequestBytes: 4096, MaxResponseBytes: maxResponse, State: "active", CreatedAt: at.Add(-time.Hour),
	}
	if auth == "header" {
		d.AuthHeaderName = "X-Provider-Key"
	}
	secret := application.Secret{}
	if auth != "none" {
		var err error
		secret, err = application.NewSecret([]byte("secret-http-token"))
		if err != nil {
			t.Fatal(err)
		}
	}
	return application.PreparedInvocation{
		WorkspaceID: "ws_http", RunID: "run_http", ToolVersionID: "toolv_http",
		Deployment: d, CanonicalArguments: `{"message":"hello"}`,
		Credential: application.CredentialReference{ConnectionID: "conn_http", ProviderID: "provider_http", CredentialVersionRef: "credv_http", Revision: 1, ValidUntil: at.Add(time.Hour)},
		Secret:     secret,
	}
}

func submission() supply.Submission {
	return supply.Submission{WorkspaceID: "ws_http", RunID: "run_http", AttemptNo: 1, Generation: 1, SubmissionKey: "mender.submit.run_http.1"}
}

func localPolicy(t *testing.T, rawURL string) httpexecutor.EgressPolicy {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return httpexecutor.EgressPolicy{AllowedHosts: []string{u.Hostname()}, AllowHTTP: true, AllowLoopback: true}
}

func TestHTTPExecutorSendsCanonicalBodyAuthAndIdempotencyToAllowedLoopback(t *testing.T) {
	requests := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected method/content headers: %s %q %q", r.Method, r.Header.Get("Content-Type"), r.Header.Get("Accept"))
		}
		if r.Header.Get("Authorization") != "Bearer secret-http-token" || r.Header.Get("Idempotency-Key") != "mender.submit.run_http.1" {
			t.Errorf("missing auth/idempotency headers")
		}
		if string(body) != `{"message":"hello"}` {
			t.Errorf("unexpected body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"provider_request_id":"req_123","external_task_id":"task_456"}`)
	}))
	defer server.Close()

	prepared := httpPrepared(t, server.URL+"/v1/run", "bearer", 4096, time.Second)
	broker := httpBrokerFunc(func(_ context.Context, ref application.InvocationRef, _ time.Time) (application.PreparedInvocation, error) {
		if ref.WorkspaceID != "ws_http" || ref.RunID != "run_http" {
			t.Fatalf("wrong broker ref: %+v", ref)
		}
		return prepared, nil
	})
	executor, err := httpexecutor.New(broker, localPolicy(t, server.URL), nil, nil, httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Submit(context.Background(), submission())
	if err != nil || result.Disposition != supply.Accepted || result.ProviderRequestID != "req_123" || result.ExternalTaskID != "task_456" || requests.Load() != 1 {
		t.Fatal(result, requests.Load(), err)
	}
}

func TestHTTPExecutorNeverFollowsRedirect(t *testing.T) {
	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL+"/secret", http.StatusFound)
	}))
	defer source.Close()
	prepared := httpPrepared(t, source.URL+"/redirect", "none", 1024, time.Second)
	executor, err := httpexecutor.New(httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
		return prepared, nil
	}), localPolicy(t, source.URL), nil, nil, httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Submit(context.Background(), submission())
	if err != nil || result.Disposition != supply.Unknown || targetCalls.Load() != 0 {
		t.Fatal(result, targetCalls.Load(), err)
	}
}

func TestHTTPExecutorTreatsTimeoutOversizeAndMalformedSuccessAsUnknown(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		maxResponse int
		timeout     time.Duration
	}{
		{"timeout", func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"provider_request_id":"late"}`)
		}, 1024, 100 * time.Millisecond},
		{"oversize", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 128))
		}, 32, time.Second},
		{"duplicate", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"provider_request_id":"a","provider_request_id":"b"}`)
		}, 1024, time.Second},
		{"unknown_field", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"provider_request_id":"a","extra":"b"}`)
		}, 1024, time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			prepared := httpPrepared(t, server.URL, "none", tc.maxResponse, tc.timeout)
			executor, err := httpexecutor.New(httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
				return prepared, nil
			}), localPolicy(t, server.URL), nil, nil, httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			result, err := executor.Submit(context.Background(), submission())
			if err != nil || result.Disposition != supply.Unknown {
				t.Fatal(result, err)
			}
		})
	}
}

func TestHTTPExecutorRejectsPrivateOrMixedDNSBeforeDial(t *testing.T) {
	prepared := httpPrepared(t, "https://provider.example/v1/run", "none", 1024, time.Second)
	broker := httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
		return prepared, nil
	})
	for _, tc := range []struct {
		name string
		ips  []net.IPAddr
	}{
		{"private", []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}},
		{"loopback", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}},
		{"cgnat", []net.IPAddr{{IP: net.ParseIP("100.64.0.2")}}},
		{"mixed", []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("127.0.0.1")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dials := atomic.Int32{}
			executor, err := httpexecutor.New(broker, httpexecutor.EgressPolicy{AllowedHosts: []string{"provider.example"}}, httpResolverFunc(func(context.Context, string) ([]net.IPAddr, error) { return tc.ips, nil }), httpDialerFunc(func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, errors.New("must not dial")
			}), httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			_, err = executor.Submit(context.Background(), submission())
			if !errors.Is(err, supply.ErrExecutorForbidden) || dials.Load() != 0 {
				t.Fatal(err, dials.Load())
			}
		})
	}
}

func TestHTTPExecutorPinsValidatedDNSAddressForDial(t *testing.T) {
	prepared := httpPrepared(t, "https://provider.example:8443/v1/run", "none", 1024, time.Second)
	var dialed string
	executor, err := httpexecutor.New(
		httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
			return prepared, nil
		}),
		httpexecutor.EgressPolicy{AllowedHosts: []string{"provider.example"}},
		httpResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}),
		httpDialerFunc(func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = address
			return nil, errors.New("synthetic dial stop")
		}),
		httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Submit(context.Background(), submission())
	if err != nil || result.Disposition != supply.Unknown || dialed != "93.184.216.34:8443" {
		t.Fatal(result, dialed, err)
	}
}

func TestHTTPExecutorDefaultDenyAndHeaderSafety(t *testing.T) {
	if _, err := httpexecutor.New(httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
		return application.PreparedInvocation{}, nil
	}), httpexecutor.EgressPolicy{}, nil, nil, nil); !errors.Is(err, supply.ErrExecutorUnavailable) {
		t.Fatal("empty allowlist did not fail closed", err)
	}
	prepared := httpPrepared(t, "https://provider.example/v1/run", "header", 1024, time.Second)
	prepared.Deployment.AuthHeaderName = "Idempotency-Key"
	executor, err := httpexecutor.New(httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
		return prepared, nil
	}), httpexecutor.EgressPolicy{AllowedHosts: []string{"provider.example"}}, httpResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}), httpDialerFunc(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("unsafe headers reached dial")
		return nil, nil
	}), httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Submit(context.Background(), submission())
	if !errors.Is(err, supply.ErrExecutorUnavailable) && !errors.Is(err, supply.ErrExecutorForbidden) {
		t.Fatal("unsafe header collision was accepted", err)
	}
}

func TestHTTPExecutorRejectsHeaderInjectionBeforeDial(t *testing.T) {
	prepared := httpPrepared(t, "https://provider.example/v1/run", "header", 1024, time.Second)
	secret, err := application.NewSecret([]byte("safe\r\nX-Injected: yes"))
	if err != nil {
		t.Fatal(err)
	}
	prepared.Secret = secret
	dials := atomic.Int32{}
	executor, err := httpexecutor.New(
		httpBrokerFunc(func(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
			return prepared, nil
		}),
		httpexecutor.EgressPolicy{AllowedHosts: []string{"provider.example"}},
		httpResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}),
		httpDialerFunc(func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("must not dial")
		}),
		httpClock{at: time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Submit(context.Background(), submission())
	if !errors.Is(err, supply.ErrExecutorForbidden) || dials.Load() != 0 {
		t.Fatal("header injection reached network", err, dials.Load())
	}
}
