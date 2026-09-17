package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/orz-i/mender/backend/internal/platform/localprovider"
)

const defaultAddr = "0.0.0.0:19080"

func health(addr string) error {
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func main() {
	if os.Getenv("MENDER_LOCAL_PROVIDER_ENABLED") != "true" {
		fmt.Fprintln(os.Stderr, "local provider fixture is disabled")
		os.Exit(2)
	}
	addr := strings.TrimSpace(os.Getenv("MENDER_LOCAL_PROVIDER_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}
	if len(os.Args) == 2 && os.Args[1] == "health" {
		if err := health(addr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: localprovider [health]")
		os.Exit(2)
	}
	server := &http.Server{Addr: addr, Handler: localprovider.New().Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
