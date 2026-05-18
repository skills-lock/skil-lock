package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/skills-lock/skil-lock/internal/scan"
)

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list [path]",
		Short: "Repo-wide inventory of detected Skills (table by default; --json for machine output).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			rep, err := scan.Repo(root)
			if err != nil {
				return err
			}
			for _, e := range rep.Errors {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", e.Path, e.Error)
			}
			inv := scan.Inventories(rep)
			if asJSON {
				return emitJSON(cmd.OutOrStdout(), inv)
			}
			return emitTable(cmd.OutOrStdout(), inv)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON instead of a human table.")
	return cmd
}

func emitJSON(w io.Writer, inv []scan.Inventory) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}

func emitTable(w io.Writer, inv []scan.Inventory) error {
	if len(inv) == 0 {
		_, err := fmt.Fprintln(w, "no skills detected")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tRUNTIME\tVERSION\tSHELL\tURLS\tPATHS\tSOURCE"); err != nil {
		return err
	}
	for _, r := range inv {
		row := strings.Join([]string{
			r.Name,
			string(r.Runtime),
			r.Version,
			fmt.Sprintf("%d", r.NumShell),
			fmt.Sprintf("%d", r.NumURLs),
			fmt.Sprintf("%d", r.NumPaths),
			r.SourcePath,
		}, "\t")
		if _, err := fmt.Fprintln(tw, row); err != nil {
			return err
		}
	}
	return tw.Flush()
}
