package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/skills-lock/skil-lock/internal/lockfile"
	"github.com/skills-lock/skil-lock/internal/model"
	"github.com/skills-lock/skil-lock/internal/scan"
)

const defaultLockfileName = "skills.lock"

func newLockCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "lock [path]",
		Short: "Write or update skills.lock under [path] with the current detected behavior.",
		Long: `lock walks [path] (default .), parses every Claude Code and Codex skill,
runs the v1 detectors, and writes the result to skills.lock at the repo
root. Any pre-existing skills.lock is overwritten; use git diff to see
what changed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			rep, err := scan.Repo(root)
			if err != nil {
				return err
			}
			if len(rep.Errors) > 0 {
				for _, e := range rep.Errors {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", e.Path, e.Error)
				}
			}

			lf := buildLockfile(rep)
			path := outPath
			if path == "" {
				path = filepath.Join(root, defaultLockfileName)
			}
			if err := lockfile.Save(lf, path); err != nil {
				return fmt.Errorf("save lockfile: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d skills)\n", path, len(lf.Skills))
			return nil
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Lockfile output path (default: <path>/skills.lock)")
	return cmd
}

// buildLockfile assembles a model.Lockfile from a scan report. The
// generated_by string carries the build's version so reviewers can
// trace which CLI release wrote the file.
func buildLockfile(rep scan.Report) model.Lockfile {
	lf := model.NewLockfile(generatorString(), time.Now())
	for _, s := range rep.Skills {
		lf.Skills[s.Identity.Name] = model.NewLockEntry(s.Identity, s.ContentHash, s.Behavior, s.ScriptHashes)
	}
	return lf
}

func generatorString() string {
	return "skil-lock " + version
}
