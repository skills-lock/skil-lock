package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/skills-lock/skil-lock/internal/scan"
)

func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan [path]",
		Short: "Parse skills under [path] and print a JSON behavior report. No file writes.",
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
			out := cmd.OutOrStdout()
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rep); err != nil {
				return fmt.Errorf("encode report: %w", err)
			}
			return nil
		},
	}
}
