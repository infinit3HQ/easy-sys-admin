package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/apiclient"
)

func probeCommand() *cobra.Command {
	var source string
	var port int
	cmd := &cobra.Command{Use: "probe <name|id>", Short: "Try a TCP connection from this machine and record the observation", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		if !probeSourceValid(source) || port < 1 || port > 65535 {
			return errors.New("set --source to a stable location label and --port to 1 through 65535")
		}
		srv, err := findServer(c.Context(), args[0])
		if err != nil {
			return err
		}
		if srv.Connection != nil && string(*srv.Connection) == "outbound" {
			return fmt.Errorf("outbound servers have no enrolled SSH address to probe")
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, dialErr := dialer.DialContext(c.Context(), "tcp", net.JoinHostPort(srv.Host, strconv.Itoa(port)))
		state, reason := "reachable", "none"
		if dialErr != nil {
			var dns *net.DNSError
			var network net.Error
			switch {
			case errors.As(dialErr, &dns):
				state, reason = "unknown", "dns_failed"
			case errors.Is(dialErr, syscall.ECONNREFUSED):
				state, reason = "not_reached", "refused"
			case errors.As(dialErr, &network) && network.Timeout():
				state, reason = "not_reached", "timeout"
			default:
				state, reason = "unknown", "network_error"
			}
		} else {
			_ = conn.Close()
		}
		if c.Context().Err() != nil {
			return c.Context().Err()
		}
		cl, err := client()
		if err != nil {
			return err
		}
		response, err := cl.SubmitReachabilityWithResponse(c.Context(), srv.Id, apiclient.ProbeInput{Source: source, Host: srv.Host, Port: port, State: apiclient.ProbeInputState(state), Reason: apiclient.ProbeInputReason(reason), ObservedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		if err := apiErr(response.HTTPResponse, response.Body); err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(c.OutOrStdout()).Encode(map[string]any{"source": source, "host": srv.Host, "port": port, "state": state, "reason": reason})
		}
		fmt.Fprintf(c.OutOrStdout(), "%s: %s TCP %d: %s (%s)\n", source, srv.Host, port, state, reason)
		return nil
	}}
	cmd.Flags().StringVar(&source, "source", "", "Stable label for this probe location, such as office or external-vps")
	cmd.Flags().IntVar(&port, "port", 0, "TCP port to check")
	return cmd
}

func probeSourceValid(s string) bool {
	if len(s) < 1 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
