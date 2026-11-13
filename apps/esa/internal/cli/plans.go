package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/apiclient"
)

// plansCmd lists, proposes and executes plans. A plan is approved or
// rejected only by a person in the web app, so there is no approve command.
func plansCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "Propose and run maintenance plans a person approves in the web app",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "Recent plans, newest first",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.ListPlansWithResponse(c.Context())
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
			fmt.Fprintln(w, "ID\tKIND\tTARGET\tSTATUS\tCREATED\tRUN")
			for _, p := range r.JSON200.Items {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", p.Id, p.Body.Kind, planTarget(p.Body), p.Status,
					p.CreatedAt.Local().Format("Jan 2 15:04"), optionalID(p.RunId))
			}
			return w.Flush()
		},
	}

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "One plan with every step it would take",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return errors.New("plan id must be a number")
			}
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.GetPlanWithResponse(c.Context(), id)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r.JSON200)
			}
			printPlan(r.JSON200)
			return nil
		},
	}

	var group string
	var servers []string
	var mode string
	propose := &cobra.Command{
		Use:   "propose <measure|clean|update>",
		Short: "Store a plan for a person to approve in the web app. It does not change a server.",
		Example: "  esa plans propose update --group production\n" +
			"  esa plans propose clean --server media-01",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			kind := apiclient.PlanBodyKind(args[0])
			switch kind {
			case apiclient.PlanBodyKindMeasure, apiclient.PlanBodyKindClean, apiclient.PlanBodyKindUpdate:
			default:
				return errors.New("kind must be measure, clean, or update")
			}
			if (group == "") == (len(servers) == 0) {
				return errors.New("give exactly one of --group or --server")
			}
			if mode != "" && mode != "security" && mode != "all" {
				return errors.New("--mode must be security or all")
			}
			cl, err := client()
			if err != nil {
				return err
			}
			body := apiclient.PlanBody{Kind: kind}
			var grp *apiclient.Group
			if group != "" {
				if grp, err = findGroup(c.Context(), cl, group); err != nil {
					return err
				}
				body.GroupId = &grp.Id
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
			fillPlanSettings(&body, grp, mode)
			r, err := cl.ProposePlanWithResponse(c.Context(), body)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r.JSON201)
			}
			fmt.Printf("Proposed plan %d. A person approves it on the Plans page, then run: esa plans execute %d\n", r.JSON201.Id, r.JSON201.Id)
			return nil
		},
	}
	propose.Flags().StringVar(&group, "group", "", "group name")
	propose.Flags().StringSliceVar(&servers, "server", nil, "server name or id (repeatable)")
	propose.Flags().StringVar(&mode, "mode", "", "for update plans: security or all (default: the group's setting)")

	execute := &cobra.Command{
		Use:   "execute <id>",
		Short: "Start an approved plan. A repeat returns the same run.",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return errors.New("plan id must be a number")
			}
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.ExecutePlanWithResponse(c.Context(), id)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r.JSON202)
			}
			fmt.Printf("Started %s run %d. Follow it with: esa runs show %d\n", r.JSON202.Kind, r.JSON202.Id, r.JSON202.Id)
			return nil
		},
	}

	cmd.AddCommand(list, show, propose, execute)
	return cmd
}

// fillPlanSettings copies the group's cleanup and update settings into the
// plan, so the plan says exactly what will run. Without a group it uses the
// control plane's defaults for a server run.
func fillPlanSettings(body *apiclient.PlanBody, grp *apiclient.Group, mode string) {
	opts := runOptionsFrom(grp)
	switch body.Kind {
	case apiclient.PlanBodyKindClean, apiclient.PlanBodyKindMeasure:
		body.BuildCache, body.Vscode, body.Journal = &opts.BuildCache, &opts.VSCode, &opts.Journal
		body.JournalSize = &opts.JournalSize
	case apiclient.PlanBodyKindUpdate:
		m := apiclient.PlanBodyUpdateMode(opts.UpdateMode)
		if mode != "" {
			m = apiclient.PlanBodyUpdateMode(mode)
		}
		body.UpdateMode = &m
		body.RebootIfRequired = &opts.RebootIfRequired
		if len(opts.Hold) > 0 {
			hold := opts.Hold
			body.Hold = &hold
		}
	}
}

func planTarget(b apiclient.PlanBody) string {
	if b.GroupId != nil {
		return "group " + strconv.FormatInt(*b.GroupId, 10)
	}
	if b.ServerIds != nil {
		return fmt.Sprintf("%d servers", len(*b.ServerIds))
	}
	return "-"
}

func optionalID(id *int64) string {
	if id == nil {
		return "-"
	}
	return strconv.FormatInt(*id, 10)
}

func printPlan(p *apiclient.FixPlan) {
	b := p.Body
	fmt.Printf("Plan %d: %s on %s, %s\n", p.Id, b.Kind, planTarget(b), p.Status)
	if b.PinnedServerIds != nil {
		fmt.Printf("Servers: %s\n", joinIDs(*b.PinnedServerIds))
	}
	if b.UpdateMode != nil {
		fmt.Printf("Update mode: %s\n", *b.UpdateMode)
	}
	if b.Hold != nil && len(*b.Hold) > 0 {
		fmt.Printf("Held packages: %s\n", strings.Join(*b.Hold, ", "))
	}
	if b.RebootIfRequired != nil && *b.RebootIfRequired {
		fmt.Println("Reboot if required: yes")
	}
	if b.BuildCache != nil {
		fmt.Printf("Cleanups: build cache %s, VS Code %s, journal %s\n", yesNo(b.BuildCache), yesNo(b.Vscode), yesNo(b.Journal))
	}
	if b.Packages != nil {
		fmt.Printf("Packages (%d): %s\n", len(*b.Packages), strings.Join(*b.Packages, ", "))
	}
	if p.Reason != "" {
		fmt.Printf("Reason: %s\n", p.Reason)
	}
	fmt.Printf("Fingerprint: %s\n", p.Fingerprint)
}

func joinIDs(ids []int64) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(out, ", ")
}

func yesNo(b *bool) string {
	if b != nil && *b {
		return "yes"
	}
	return "no"
}
