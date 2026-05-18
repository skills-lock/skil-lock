package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/skills-lock/skil-lock/internal/diff"
	"github.com/skills-lock/skil-lock/internal/lockfile"
)

func newDiffCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "diff <baseline.lock> <current.lock>",
		Short: "Render a capability-delta report between two lockfiles.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldPath, newPath := args[0], args[1]
			old, err := lockfile.Load(oldPath)
			if err != nil {
				return fmt.Errorf("load baseline: %w", err)
			}
			cur, err := lockfile.Load(newPath)
			if err != nil {
				return fmt.Errorf("load current: %w", err)
			}
			d := diff.Compare(old, cur, oldPath, newPath)
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(d)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), diff.RenderMarkdown(d, ""))
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON instead of markdown.")
	return cmd
}
