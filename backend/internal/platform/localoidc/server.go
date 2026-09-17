package localoidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Server is deliberately limited to the loopback Mender demo contract. It is
// not an identity provider for production or shared environments.
type Server struct {
	issuer       string
	clientID     string
	clientSecret string
	redirects    map[string]bool
	privateKey   *rsa.PrivateKey
	keyID        string
	mu           sync.Mutex
	codes        map[string]authorizationCode
}

type authorizationCode struct {
	account     string
	nonce       string
	challenge   string
	redirectURI string
	expiresAt   time.Time
}

type Config struct {
	Issuer           string
	ClientID         string
	ClientSecretFile string
	RedirectURIs     []string
}

func New(config Config) (*Server, error) {
	issuer, err := url.Parse(config.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Hostname() != "idp.localhost" || issuer.RawQuery != "" || issuer.Fragment != "" || issuer.Path != "" {
		return nil, errors.New("local OIDC fixture requires https://idp.localhost[:port]")
	}
	if config.ClientID != "mender-local-demo" || len(config.RedirectURIs) != 2 {
		return nil, errors.New("local OIDC fixture identity is fixed")
	}
	secretBytes, err := os.ReadFile(config.ClientSecretFile)
	if err != nil || len(secretBytes) < 32 || len(secretBytes) > 256 {
		return nil, errors.New("local OIDC fixture client secret unavailable")
	}
	redirects := make(map[string]bool, 2)
	for _, raw := range config.RedirectURIs {
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.Scheme != "https" || u.Path != "/auth/callback" || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "console.localhost" && u.Hostname() != "admin.localhost") {
			return nil, errors.New("local OIDC fixture redirect must be a Mender localhost callback")
		}
		redirects[u.String()] = true
	}
	if len(redirects) != 2 {
		return nil, errors.New("local OIDC fixture requires distinct Console and Admin callbacks")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Server{issuer: config.Issuer, clientID: config.ClientID, clientSecret: strings.TrimSpace(string(secretBytes)), redirects: redirects, privateKey: key, keyID: "mender-local-demo", codes: map[string]authorizationCode{}}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("/keys", s.keys)
	mux.HandleFunc("/authorize", s.authorize)
	mux.HandleFunc("/token", s.token)
	mux.HandleFunc("/healthz", s.health)
	return mux
}

func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeJSON(w, map[string]any{"issuer": s.issuer, "authorization_endpoint": s.issuer + "/authorize", "token_endpoint": s.issuer + "/token", "jwks_uri": s.issuer + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
}

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.privateKey.PublicKey.E)).Bytes())
	n := base64.RawURLEncoding.EncodeToString(s.privateKey.PublicKey.N.Bytes())
	s.writeJSON(w, map[string]any{"keys": []map[string]string{{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": s.keyID, "n": n, "e": e}}})
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if q.Get("client_id") != s.clientID || q.Get("response_type") != "code" || !s.redirects[redirect] || q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "invalid local OIDC request", http.StatusBadRequest)
		return
	}
	account := q.Get("account")
	if account == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		_, _ = fmt.Fprintf(w, "<!doctype html><meta name=viewport content='width=device-width'><title>Mender Local OIDC</title><style>body{font:16px system-ui;max-width:42rem;margin:4rem auto;padding:1rem}a{display:block;margin:1rem 0;padding:.8rem 1rem;border:1px solid #bbb;border-radius:8px;text-decoration:none;color:#163044}</style><h1>Mender 本地测试登录</h1><p>仅供本机 Docker 测试。选择身份：</p>%s%s", s.accountLink(r.URL, "maker", "Maker / Workspace Owner"), s.accountLink(r.URL, "checker", "Checker / Platform Reviewer"))
		return
	}
	if account != "maker" && account != "checker" {
		http.Error(w, "unknown local account", http.StatusBadRequest)
		return
	}
	code, err := randomToken(24)
	if err != nil {
		http.Error(w, "local OIDC unavailable", http.StatusServiceUnavailable)
		return
	}
	s.mu.Lock()
	s.codes[code] = authorizationCode{account: account, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: redirect, expiresAt: time.Now().Add(2 * time.Minute)}
	s.mu.Unlock()
	destination, _ := url.Parse(redirect)
	values := destination.Query()
	values.Set("code", code)
	values.Set("state", q.Get("state"))
	destination.RawQuery = values.Encode()
	http.Redirect(w, r, destination.String(), http.StatusFound)
}

func (s *Server) accountLink(source *url.URL, account, label string) string {
	next := *source
	q := next.Query()
	q.Set("account", account)
	next.RawQuery = q.Encode()
	return `<a href="` + html.EscapeString(next.String()) + `">` + html.EscapeString(label) + `</a>`
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" {
		http.Error(w, "invalid token request", http.StatusBadRequest)
		return
	}
	clientID, secret, ok := r.BasicAuth()
	if !ok {
		clientID, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	if clientID != s.clientID || secret != s.clientSecret {
		http.Error(w, "invalid client", http.StatusUnauthorized)
		return
	}
	code := r.Form.Get("code")
	s.mu.Lock()
	entry, exists := s.codes[code]
	delete(s.codes, code)
	s.mu.Unlock()
	if !exists || time.Now().After(entry.expiresAt) || entry.redirectURI != r.Form.Get("redirect_uri") {
		http.Error(w, "invalid authorization code", http.StatusBadRequest)
		return
	}
	challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(challenge[:]) != entry.challenge {
		http.Error(w, "PKCE verification failed", http.StatusBadRequest)
		return
	}
	now := time.Now().Unix()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": s.keyID, "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": s.issuer, "aud": s.clientID, "sub": entry.account, "nonce": entry.nonce, "iat": now, "exp": now + 120})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		http.Error(w, "local OIDC unavailable", http.StatusServiceUnavailable)
		return
	}
	access, _ := randomToken(24)
	s.writeJSON(w, map[string]any{"access_token": "local-" + access, "token_type": "Bearer", "expires_in": 120, "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeJSON(w, map[string]string{"service": "mender-local-oidc", "status": "ready"})
}

func (s *Server) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Serve(ctx context.Context, addr, certFile, keyFile string, server *Server) error {
	if server == nil || addr == "" || certFile == "" || keyFile == "" {
		return errors.New("local OIDC server configuration incomplete")
	}
	httpServer := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServeTLS(certFile, keyFile) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func Check(ctx context.Context, issuer, caFile string) error {
	endpoint, err := url.Parse(issuer + "/healthz")
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() != "idp.localhost" {
		return errors.New("invalid local OIDC health endpoint")
	}
	caBytes, err := os.ReadFile(caFile)
	if err != nil {
		return errors.New("local OIDC CA unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return errors.New("local OIDC CA invalid")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return errors.New("local OIDC healthcheck failed")
	}
	var value map[string]string
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4096))
	if decoder.Decode(&value) != nil || value["service"] != "mender-local-oidc" || value["status"] != "ready" {
		return errors.New("local OIDC health identity invalid")
	}
	return nil
}
