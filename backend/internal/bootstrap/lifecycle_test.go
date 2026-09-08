package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestWorkerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunWorker(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop on cancellation")
	}
}

func TestAPIRejectsInvalidAddress(t *testing.T) {
	t.Setenv("MENDER_HTTP_ADDR", "127.0.0.1:invalid-port")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := RunAPI(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("invalid address must fail startup")
	}
}
