// esa-agent connects out to the control plane and invokes only esa-helper.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/agent"
)

func main() {
	token, err := agent.ReadToken(os.Getenv("ESA_AGENT_TOKEN_FILE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client, err := agent.New(os.Getenv("ESA_AGENT_URL"), token)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err = client.Run(ctx, agent.Execute); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "outbound agent stopped")
		os.Exit(1)
	}
}
