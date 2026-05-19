// Command skil-lock pins approved AI Skill behavior and blocks unapproved
// drift in CI.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Injected at build time via -ldflags. See Makefile.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "skil-lock",
		Short:         "Pin approved AI Skill behavior; block unapproved drift in CI.",
		Long:          "SkilLock scans your repo's Claude Code and Codex Skills, records the approved capability surface in a committed skills.lock, and gates PRs on capability deltas.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newScanCmd())
	root.AddCommand(newLockCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newCICmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "skil-lock %s (commit %s, built %s)\n", version, commit, date)
			return err
		},
	}
}
