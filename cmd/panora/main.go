package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/vadim82/panora/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, context.Canceled) {
		os.Exit(1)
	}
}
