package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/orz-i/mender/backend/internal/platform/httpserver"
)

// RunAPI is the composition root for the API process. It exposes probes only.
func RunAPI(ctx context.Context, logger *slog.Logger) error {
	address := os.Getenv("MENDER_HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:18080"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           httpserver.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	logger.Info("api starting", "address", address, "stage", "bootstrap")
	select {
	case err := <-failures:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		logger.Info("api stopped gracefully")
		return nil
	}
}
