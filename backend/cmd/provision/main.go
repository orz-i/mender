// provision is an explicit offline/operator deployment entry, never a public API.
package main

import (
	"context"
	"fmt"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := bootstrap.RunProvision(ctx, os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
