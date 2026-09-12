package oauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
	"golang.org/x/oauth2"
)

type Config struct {
	ProviderID, AuthorizationURL, TokenURL, ClientID, ClientSecret, RedirectURL string
	Scopes                                                                      []string
}

type Client struct {
	providerID string
	config     oauth2.Config
	http       *http.Client
}

func safeHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host != "" && host != "localhost" && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local") && net.ParseIP(host) == nil
}

func safeRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && safeHTTPS(raw) && u.RawQuery == "" && u.Path == "/api/console/v1/connections/oauth/callback"
}

func publicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func reviewedHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 4 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("Connection OAuth egress target is invalid")
		}
		resolved, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(resolved) == 0 {
			return nil, errors.New("Connection OAuth egress resolution failed")
		}
		for _, ip := range resolved {
			if !publicIP(ip) {
				return nil, errors.New("Connection OAuth egress resolved to a non-public address")
			}
		}
		var last error
		for _, ip := range resolved {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			last = dialErr
		}
		return nil, last
	}
	return &http.Client{
		Timeout:   7 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func New(c Config, client *http.Client) (*Client, error) {
	if !validID(c.ProviderID) || !safeHTTPS(c.AuthorizationURL) || !safeHTTPS(c.TokenURL) || c.ClientID == "" || c.ClientSecret == "" || !safeRedirect(c.RedirectURL) || len(c.Scopes) < 1 || len(c.Scopes) > 16 {
		return nil, errors.New("Connection OAuth provider is not safely configured")
	}
	seen := map[string]bool{}
	for _, scope := range c.Scopes {
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, " \t\r\n") || seen[scope] {
			return nil, errors.New("Connection OAuth scopes are invalid")
		}
		seen[scope] = true
	}
	if client == nil {
		client = reviewedHTTPClient()
	}
	if client.Timeout <= 0 || client.Timeout > 10*time.Second {
		return nil, errors.New("Connection OAuth HTTP timeout is invalid")
	}
	reviewed := *client
	reviewed.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	config := oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Scopes:       append([]string(nil), c.Scopes...),
		Endpoint: oauth2.Endpoint{
			AuthURL:   c.AuthorizationURL,
			TokenURL:  c.TokenURL,
			AuthStyle: oauth2.AuthStyleInHeader,
		},
	}
	return &Client{providerID: c.ProviderID, config: config, http: &reviewed}, nil
}

func (c *Client) ProviderID() string { return c.providerID }

func (c *Client) AuthorizationURL(state, verifier string) (string, error) {
	if state == "" || verifier == "" {
		return "", errors.New("OAuth challenge is invalid")
	}
	return c.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), nil
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (application.OAuthAccessToken, error) {
	if code == "" || verifier == "" {
		return application.OAuthAccessToken{}, errors.New("OAuth callback is invalid")
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.http)
	token, err := c.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.Expiry.IsZero() {
		return application.OAuthAccessToken{}, errors.New("OAuth code exchange failed")
	}
	return application.OAuthAccessToken{Value: []byte(token.AccessToken), ExpiresAt: token.Expiry}, nil
}

var _ application.OAuthProvider = (*Client)(nil)
