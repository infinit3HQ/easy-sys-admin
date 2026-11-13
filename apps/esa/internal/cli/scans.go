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
			// A full Grype report can be tens of MB; only a few fields are
			// sent, so read up to 200 MiB here.
			raw, err := io.ReadAll(io.LimitReader(in, 200<<20+1))
			if err != nil {
				return err
			}
			if len(raw) > 200<<20 {
				return errors.New("the report is larger than 200 MiB")
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				return fmt.Errorf("the report is not JSON: %w", err)
			}
			body = slimGrype(body)
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

// slimGrype keeps only the fields esa reads, so a large report fits the
// import limit. A real 69 MB report slimmed to 5 MB.
func slimGrype(r map[string]any) map[string]any {
	pick := func(m any, keys ...string) map[string]any {
		src, _ := m.(map[string]any)
		out := map[string]any{}
		for _, k := range keys {
			if v, ok := src[k]; ok {
				out[k] = v
			}
		}
		return out
	}
	desc := pick(r["descriptor"], "name", "version", "db")
	matches, _ := r["matches"].([]any)
	slim := make([]any, 0, len(matches))
	for _, m := range matches {
		mm, _ := m.(map[string]any)
		related := []any{}
		if rel, ok := mm["relatedVulnerabilities"].([]any); ok {
			for _, x := range rel {
				related = append(related, pick(x, "id"))
			}
		}
		slim = append(slim, map[string]any{
			"vulnerability":          pick(mm["vulnerability"], "id", "severity", "fix", "knownExploited", "epss"),
			"relatedVulnerabilities": related,
			"artifact":               pick(mm["artifact"], "name", "version", "type"),
		})
	}
	return map[string]any{"descriptor": desc, "matches": slim}
}
