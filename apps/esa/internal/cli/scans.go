package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// scanCommands adds the SBOM export and the scan import to `esa servers`.
// The scanner runs wherever the operator chooses; esa only hands over the
// package list and stores the result.
func scanCommands() []*cobra.Command {
	sbom := &cobra.Command{
		Use:     "sbom <name|id>",
		Short:   "Write the server's installed packages as a CycloneDX SBOM to stdout",
		Example: "  esa servers sbom web-01 > web-01.cdx.json\n  grype sbom:web-01.cdx.json -o json > web-01.grype.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			s, err := findServer(c.Context(), args[0])
			if err != nil {
				return err
			}
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.GetServerSbomWithResponse(c.Context(), s.Id)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			_, err = os.Stdout.Write(r.Body)
			return err
		},
	}
	imp := &cobra.Command{
		Use:     "scan-import <name|id> <grype.json|->",
		Short:   "Import a Grype JSON report made from the server's SBOM",
		Example: "  esa servers scan-import web-01 web-01.grype.json",
		Args:    cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			s, err := findServer(c.Context(), args[0])
			if err != nil {
				return err
			}
			var in io.Reader = os.Stdin
			if args[1] != "-" {
				f, err := os.Open(args[1])
				if err != nil {
					return err
				}
				defer f.Close()
				in = f
			}
			raw, err := io.ReadAll(io.LimitReader(in, 10<<20+1))
			if err != nil {
				return err
			}
			if len(raw) > 10<<20 {
				return errors.New("the report is larger than 10 MiB")
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				return fmt.Errorf("the report is not JSON: %w", err)
			}
			cl, err := client()
			if err != nil {
				return err
			}
			r, err := cl.ImportServerScanWithResponse(c.Context(), s.Id, body)
			if err != nil {
				return err
			}
			if err := apiErr(r.HTTPResponse, r.Body); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r.JSON200)
			}
			rep := r.JSON200
			fmt.Printf("Imported %d matches from %s %s.\n%s\n", len(rep.Matches), rep.Scanner, rep.ScannerVersion, rep.Coverage)
			return nil
		},
	}
	return []*cobra.Command{sbom, imp}
}
