// esa-agent connects out to the control plane and invokes only esa-helper.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/agent"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
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
	// The pin comes from the installer, out of band. With it, the agent runs
	// only commands the control plane signed for this server.
	if raw := os.Getenv("ESA_AGENT_PIN"); raw != "" {
		pin, err := proto.ParseAgentPin(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		client.WithPin(pin)
	} else {
		fmt.Fprintln(os.Stderr, "esa-agent: ESA_AGENT_PIN is not set, so commands are not verified; reinstall with a signing pin")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err = client.Run(ctx, agent.Execute); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "outbound agent stopped")
		os.Exit(1)
	}
}
