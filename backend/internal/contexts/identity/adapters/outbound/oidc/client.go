package oidc

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
}

type Client struct {
	oauth    oauth2.Config
	verifier *coreoidc.IDTokenVerifier
}

func New(ctx context.Context, c Config) (*Client, error) {
	if !strings.HasPrefix(c.Issuer, "https://") || c.ClientID == "" || c.ClientSecret == "" || c.RedirectURL == "" {
		return nil, errors.New("OIDC client is not safely configured")
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	discoveryCtx := coreoidc.ClientContext(ctx, httpClient)
	provider, err := coreoidc.NewProvider(discoveryCtx, c.Issuer)
	if err != nil {
		return nil, errors.New("OIDC provider discovery unavailable")
	}
	oauth := oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.RedirectURL, Endpoint: provider.Endpoint(), Scopes: []string{coreoidc.ScopeOpenID, coreoidc.ScopeProfile, coreoidc.ScopeEmail}}
	return &Client{oauth: oauth, verifier: provider.Verifier(&coreoidc.Config{ClientID: c.ClientID})}, nil
}

func (c *Client) AuthorizationURL(state, nonce, verifier string) string {
	return c.oauth.AuthCodeURL(state, coreoidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

func (c *Client) Exchange(ctx context.Context, code, verifier, expectedNonce string) (application.VerifiedOIDCIdentity, error) {
	token, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return application.VerifiedOIDCIdentity{}, errors.New("OIDC code exchange failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return application.VerifiedOIDCIdentity{}, errors.New("OIDC ID token missing")
	}
	idToken, err := c.verifier.Verify(ctx, raw)
	if err != nil {
		return application.VerifiedOIDCIdentity{}, errors.New("OIDC ID token verification failed")
	}
	var claims struct {
		Nonce string `json:"nonce"`
	}
	if err = idToken.Claims(&claims); err != nil || claims.Nonce == "" || claims.Nonce != expectedNonce || idToken.Subject == "" || idToken.Issuer == "" {
		return application.VerifiedOIDCIdentity{}, errors.New("OIDC identity claims invalid")
	}
	return application.VerifiedOIDCIdentity{Issuer: idToken.Issuer, Subject: idToken.Subject}, nil
}

var _ application.OIDCClient = (*Client)(nil)
