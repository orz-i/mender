package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/orz-i/mender/backend/internal/platform/localoidc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if len(os.Args) == 2 && os.Args[1] == "health" {
		if err := localoidc.Check(ctx, os.Getenv("MENDER_LOCAL_OIDC_ISSUER"), os.Getenv("MENDER_LOCAL_OIDC_CA_FILE")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "local OIDC fixture health failed:", err)
			os.Exit(1)
		}
		return
	}
	redirects := strings.Split(os.Getenv("MENDER_LOCAL_OIDC_REDIRECT_URIS"), ",")
	server, err := localoidc.New(localoidc.Config{Issuer: os.Getenv("MENDER_LOCAL_OIDC_ISSUER"), ClientID: os.Getenv("MENDER_LOCAL_OIDC_CLIENT_ID"), ClientSecretFile: os.Getenv("MENDER_LOCAL_OIDC_CLIENT_SECRET_FILE"), RedirectURIs: redirects})
	if err == nil {
		err = localoidc.Serve(ctx, os.Getenv("MENDER_LOCAL_OIDC_ADDR"), os.Getenv("MENDER_LOCAL_OIDC_CERT_FILE"), os.Getenv("MENDER_LOCAL_OIDC_KEY_FILE"), server)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "local OIDC fixture unavailable:", err)
		os.Exit(1)
	}
}
