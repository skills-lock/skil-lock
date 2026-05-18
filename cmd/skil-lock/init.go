package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/skills-lock/skil-lock/internal/lockfile"
	"github.com/skills-lock/skil-lock/internal/scan"
)

func newInitCmd() *cobra.Command {
	var baseline bool
	var outPath string

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialise skills.lock by accepting the current state as approved.",
		Long: `init --baseline walks [path] and writes a skills.lock that captures the
current detected behavior of every Claude Code and Codex skill. It is the
first-run onboarding path for teams that already have skills in their
repo: rather than auditing them all on day one, init records "this is
what is approved as of now" and lets the diff workflow catch every change
from this point forward.

Without --baseline this is currently a no-op — there is no other init
mode in v1. The flag is required to avoid accidentally rubber-stamping a
repo.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !baseline {
				return errors.New("init requires --baseline in v1 (no other init mode yet)")
			}
			root := "."
			if len(args) == 1 {
				root = args[0]
			}

			out := outPath
			if out == "" {
				out = filepath.Join(root, defaultLockfileName)
			}
			if _, err := os.Stat(out); err == nil {
				return fmt.Errorf("%s already exists; refusing to overwrite (use `skil-lock lock` to refresh)", out)
			}

			rep, err := scan.Repo(root)
			if err != nil {
				return err
			}
			for _, e := range rep.Errors {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", e.Path, e.Error)
			}

			lf := buildLockfile(rep)
			if err := lockfile.Save(lf, out); err != nil {
				return fmt.Errorf("save lockfile: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "baseline written: %s (%d skills)\n", out, len(lf.Skills))
			return nil
		},
	}
	cmd.Flags().BoolVar(&baseline, "baseline", false, "Accept the current detected behavior as approved.")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Lockfile output path (default: <path>/skills.lock)")
	return cmd
}
