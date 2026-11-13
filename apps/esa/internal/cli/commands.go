package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/apiclient"
)

var jsonOut bool

func addClientCommands(root *cobra.Command) {
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "print JSON instead of tables")
	root.AddCommand(loginCmd(), logoutCmd(), statusCmd(), serversCmd(), groupsCmd(), runsCmd(), plansCmd(), alertsCmd())
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func table(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 2, 2, ' ', 0) }

func loginCmd() *cobra.Command {
	var token string
	cmd := &cobra.Command{
		Use:   "login <url>",
		Short: "Save the control plane URL and an API token",
		Example: "  esa login https://esa.example.com --token esa_...\n" +
			"  ESA_TOKEN=esa_... esa login https://esa.example.com",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := normalizeURL(args[0])
			if err != nil {
				return err
			}
			if token == "" {
				token = os.Getenv("ESA_TOKEN")
			}
			if !strings.HasPrefix(token, "esa_") {
				return errors.New("give an API token with --token (create one in Settings > API tokens)")
			}
			c, err := newClient(cliConfig{URL: u, Token: token})
			if err != nil {
				return err
			}
			me, err := c.GetMeWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			if err := apiErr(me.HTTPResponse, me.Body); err != nil {
				return err
			}
			path, err := saveConfig(cliConfig{URL: u, Token: token})
			if err != nil {
				return err
			}
			fmt.Printf("Logged in to %s as %s, account %q (%s).\nSaved to %s\n",
				u, me.JSON200.User.Email, me.JSON200.Account.Name, me.JSON200.Account.Role, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "API token (esa_...)")
	return cmd
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the saved token",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			p, err := configPath()
			if err != nil {
				return err
			}
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			fmt.Println("Logged out. Revoke the token in Settings if it may have leaked.")
			return nil
		},
	}
}

func overview(ctx context.Context) (*apiclient.FleetOverview, error) {
	c, err := client()
	if err != nil {
		return nil, err
	}
	r, err := c.GetFleetOverviewWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := apiErr(r.HTTPResponse, r.Body); err != nil {
		return nil, err
	}
	return r.JSON200, nil
}

func pct(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + "%" }

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show every server's status, worst first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ov, err := overview(cmd.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(ov)
			}
			t := ov.Totals
			fmt.Printf("%d servers: %d ok, %d warning, %d failing, %d unreachable, %d pending\n\n",
				t.Servers, t.Ok, t.Warning, t.Failing, t.Unreachable, t.Pending)
			w := table(os.Stdout)
			fmt.Fprintln(w, "STATUS\tSERVER\tGROUP\tCPU\tMEM\tDISK\tUPDATES\tWHY")
			for _, s := range ov.Servers {
				cpu, mem, disk := "-", "-", "-"
				if m := s.Metrics; m != nil {
					cpu, mem, disk = pct(m.CpuPercent), pct(m.MemPercent), pct(m.DiskPercent)
				}
				upd := "-"
				if s.Updates != nil {
					upd = fmt.Sprintf("%d sec / %d", s.Updates.Security, s.Updates.Other)
				}
				why := strings.Join(s.Reasons, "; ")
				if s.Status == apiclient.ServerStatusUnreachable {
					why = s.LastError
				}
				group := "-"
				if s.GroupName != nil {
					group = *s.GroupName
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.Status, s.Name, group, cpu, mem, disk, upd, why)
			}
			return w.Flush()
		},
	}
}

// findServer resolves a name or id.
func findServer(ctx context.Context, ref string) (apiclient.Server, error) {
	ov, err := overview(ctx)
	if err != nil {
		return apiclient.Server{}, err
	}
	for _, s := range ov.Servers {
		if strings.EqualFold(s.Name, ref) || strconv.FormatInt(s.Id, 10) == ref {
			return s, nil
		}
	}
	return apiclient.Server{}, fmt.Errorf("no server named %q", ref)
}

func serversCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "servers", Short: "List and inspect servers"}
	cmd.AddCommand(scanCommands()...)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List servers",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ov, err := overview(c.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(ov.Servers)
			}
			w := table(os.Stdout)
			fmt.Fprintln(w, "ID\tNAME\tHOST\tSTATUS\tHELPER\tLAST SEEN")
			for _, s := range ov.Servers {
				seen := "never"
				if s.LastSeenAt != nil {
					seen = time.Since(*s.LastSeenAt).Round(time.Second).String() + " ago"
				}
				fmt.Fprintf(w, "%d\t%s\t%s:%d\t%s\t%s\t%s\n", s.Id, s.Name, s.Host, s.Port, s.Status, s.HelperVersion, seen)
			}
			return w.Flush()
		},
	}, &cobra.Command{
		Use:   "show <name|id>",
		Short: "Show one server's latest snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			s, err := findServer(c.Context(), args[0])
			if err != nil {
				return err
			}
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.GetServerWithResponse(c.Context(), s.Id)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			d := r.JSON200
			if jsonOut {
				return printJSON(d)
			}
			fmt.Printf("%s  %s:%d  %s\n", d.Server.Name, d.Server.Host, d.Server.Port, d.Server.Status)
			for _, why := range d.Server.Reasons {
				fmt.Println("  -", why)
			}
			if snap := d.Snapshot; snap != nil {
				fmt.Printf("\n%s, kernel %s, up %s\n", snap.OS, snap.Kernel, (time.Duration(snap.UptimeSeconds) * time.Second).Round(time.Hour))
				fmt.Printf("cpu %.1f%%  load %.2f %.2f %.2f  memory %s of %s\n", snap.CPUPercent, snap.Load1, snap.Load5, snap.Load15, human(snap.MemUsed), human(snap.MemTotal))
				w := table(os.Stdout)
				fmt.Fprintln(w, "\nMOUNT\tUSED\tSIZE\tUSE")
				for _, dk := range snap.Disks {
					use := 0.0
					if dk.Total > 0 {
						use = 100 * float64(dk.Used) / float64(dk.Total)
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%.0f%%\n", dk.Mount, human(dk.Used), human(dk.Total), use)
				}
				_ = w.Flush()
			}
			if u := d.PendingUpdates; u != nil {
				fmt.Printf("\n%d pending updates, %d security\n", len(u.Packages), u.Security)
			}
			return nil
		},
	})
	return cmd
}

func human(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func groupsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "groups", Short: "List groups"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List groups and their schedules",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.ListGroupsWithResponse(c.Context())
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r.JSON200.Items)
			}
			w := table(os.Stdout)
			fmt.Fprintln(w, "ID\tNAME\tSERVERS\tCLEAN\tUPDATES\tTIMEZONE")
			for _, g := range r.JSON200.Items {
				clean, upd := "off", "off"
				if g.Clean.Enabled {
					clean = fmt.Sprintf("%v %s", g.Clean.Days, g.Clean.Time)
				}
				if g.Updates.Enabled {
					upd = fmt.Sprintf("%s %v %s", g.Updates.Mode, g.Updates.Days, g.Updates.Time)
				}
				fmt.Fprintf(w, "%d\t%s\t%d\t%s\t%s\t%s\n", g.Id, g.Name, g.ServerCount, clean, upd, g.Timezone)
			}
			return w.Flush()
		},
	})
	return cmd
}
