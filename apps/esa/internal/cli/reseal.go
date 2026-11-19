package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func newResealCommand() *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "reseal",
		Short: "Re-seal stored secrets with the first ESA_MASTER_KEY",
		Long: `Re-seal account SSH keys, saved server logins, and notification channel
settings with the first key in ESA_MASTER_KEY. Every key in the list must
still be able to open the old secrets. --dry-run counts the work and writes
nothing. A row that cannot be opened is left unchanged. Old database backups
still need the key they were sealed with.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.FromEnv()
			if err != nil {
				return err
			}
			box, err := crypto.NewBox(cfg.MasterKeys...)
			if err != nil {
				return err
			}
			log := newLogger(cfg.LogFormat)
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL, log)
			if err != nil {
				return err
			}
			defer st.Close()
			svc := fleet.New(cmd.Context(), fleet.Deps{Store: st, Box: box, Logger: log})
			rep, err := svc.Reseal(cmd.Context(), dry)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "current key %s\n", rep.CurrentKey)
			for _, c := range rep.Classes {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %d total, %d current, %d resealed, %d left, %d failed\n",
					c.Name, c.Total, c.Current, c.Resealed, c.Left, c.Failed)
			}
			if dry {
				fmt.Fprintln(cmd.OutOrStdout(), "dry run, nothing written")
			}
			if !rep.Ready {
				return fmt.Errorf("re-seal is not complete")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "live rows are sealed with the current key")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dry, "dry-run", false, "count rows and write nothing")
	return cmd
}
