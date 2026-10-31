package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/apiclient"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func runsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "runs", Short: "Start and follow maintenance runs"}

	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "Recent runs, newest first",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.ListRunsWithResponse(c.Context(), &apiclient.ListRunsParams{Limit: &limit})
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
			fmt.Fprintln(w, "ID\tKIND\tTARGET\tSTATUS\tSTARTED\tSUMMARY")
			for _, run := range r.JSON200.Items {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", run.Id, run.Kind, target(run), run.Status, run.StartedAt.Local().Format("Jan 2 15:04"), run.Summary)
			}
			return w.Flush()
		},
	}
	list.Flags().IntVar(&limit, "limit", 20, "how many runs")

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "One run with each server's result",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("run id must be a number")
			}
			run, err := getRun(c.Context(), id)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(run)
			}
			printRun(run)
			return nil
		},
	}

	var group string
	var servers []string
	var wait bool
	var mode string
	start := &cobra.Command{
		Use:   "start <measure|clean|update|reboot>",
		Short: "Start a run on a group or on servers",
		Example: "  esa runs start measure --group production\n" +
			"  esa runs start clean --server media-01 --wait\n" +
			"  esa runs start update --group production --mode security",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			kind := apiclient.RunKind(args[0])
			switch kind {
			case apiclient.Measure, apiclient.Clean, apiclient.Update, apiclient.Reboot:
			default:
				return errors.New("kind must be measure, clean, update, or reboot")
			}
			if (group == "") == (len(servers) == 0) {
				return errors.New("give exactly one of --group or --server")
			}
			cl, err := client()
			if err != nil {
				return err
			}
			body := apiclient.StartRunJSONRequestBody{Kind: kind}
			if group != "" {
				gid, err := findGroup(c.Context(), cl, group)
				if err != nil {
					return err
				}
				body.GroupId = &gid
			} else {
				var ids []int64
				for _, ref := range servers {
					s, err := findServer(c.Context(), ref)
					if err != nil {
						return err
					}
					ids = append(ids, s.Id)
				}
				body.ServerIds = &ids
			}
			if mode != "" && kind == apiclient.Update {
				opts := apiclient.RunOptions{JournalSize: "200M"}
				if mode != "security" && mode != "all" {
					return errors.New("--mode must be security or all")
				}
				opts.UpdateMode = proto.UpdateMode(mode)
				body.Options = &opts
			}
			r, err := cl.StartRunWithResponse(c.Context(), body)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			run := r.JSON202
			if !wait {
				if jsonOut {
					return printJSON(run)
				}
				fmt.Printf("Started %s run %d. Follow it with: esa runs show %d\n", run.Kind, run.Id, run.Id)
				return nil
			}
			fmt.Fprintf(os.Stderr, "Run %d started, waiting", run.Id)
			for run.Status == "running" {
				select {
				case <-c.Context().Done():
					return c.Context().Err()
				case <-time.After(3 * time.Second):
				}
				fmt.Fprint(os.Stderr, ".")
				if run, err = getRun(c.Context(), run.Id); err != nil {
					return err
				}
			}
			fmt.Fprintln(os.Stderr)
			if jsonOut {
				return printJSON(run)
			}
			printRun(run)
			if run.Status != "succeeded" {
				return fmt.Errorf("run %d %s", run.Id, run.Status)
			}
			return nil
		},
	}
	start.Flags().StringVar(&group, "group", "", "group name")
	start.Flags().StringSliceVar(&servers, "server", nil, "server name or id (repeatable)")
	start.Flags().BoolVar(&wait, "wait", false, "wait for the run to finish and exit non-zero if it fails")
	start.Flags().StringVar(&mode, "mode", "", "for update runs: security or all (default: the group's setting)")

	cmd.AddCommand(list, show, start)
	return cmd
}

func findGroup(ctx context.Context, cl *apiclient.ClientWithResponses, name string) (int64, error) {
	r, err := cl.ListGroupsWithResponse(ctx)
	if err != nil {
		return 0, err
	}
	if err := apiErr(r.HTTPResponse, r.Body); err != nil {
		return 0, err
	}
	for _, g := range r.JSON200.Items {
		if strings.EqualFold(g.Name, name) || strconv.FormatInt(g.Id, 10) == name {
			return g.Id, nil
		}
	}
	return 0, fmt.Errorf("no group named %q", name)
}

func getRun(ctx context.Context, id int64) (*apiclient.Run, error) {
	cl, err := client()
	if err != nil {
		return nil, err
	}
	r, err := cl.GetRunWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := apiErr(r.HTTPResponse, r.Body); err != nil {
		return nil, err
	}
	return r.JSON200, nil
}

func target(run apiclient.Run) string {
	if run.GroupName != nil {
		return *run.GroupName
	}
	names := make([]string, 0, len(run.Servers))
	for _, s := range run.Servers {
		names = append(names, s.ServerName)
	}
	return strings.Join(names, ",")
}

func printRun(run *apiclient.Run) {
	fmt.Printf("Run %d: %s on %s, %s\n%s\n\n", run.Id, run.Kind, target(*run), run.Status, run.Summary)
	w := table(os.Stdout)
	fmt.Fprintln(w, "SERVER\tSTATUS\tFREED\tERROR")
	for _, s := range run.Servers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ServerName, s.Status, human(uint64(max(s.BytesFreed, 0))), s.Error)
	}
	_ = w.Flush()
}

func alertsCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Open alerts (or recent ones with --all)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.ListAlertsWithResponse(c.Context(), &apiclient.ListAlertsParams{All: &all})
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r.JSON200.Items)
			}
			if len(r.JSON200.Items) == 0 {
				fmt.Println("No open alerts.")
				return nil
			}
			w := table(os.Stdout)
			fmt.Fprintln(w, "SEVERITY\tKIND\tMESSAGE\tSINCE")
			for _, a := range r.JSON200.Items {
				since := a.PendingSince
				if a.FiredAt != nil {
					since = *a.FiredAt
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Severity, a.Kind, a.Message, since.Local().Format("Jan 2 15:04"))
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include resolved alerts")
	return cmd
}
