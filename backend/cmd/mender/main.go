package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/orz-i/mender/backend/internal/platform/mcpconsumer"
)

func main() {
	ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt)
	code:=mcpconsumer.Run(ctx,os.Args[1:],os.Stdin,os.Stdout,os.Stderr)
	cancel()
	os.Exit(code)
}
