package main

import (
	"context"
	"os"
	"os/signal"

	"vld.bg/bichme/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd.Execute(ctx)
}
