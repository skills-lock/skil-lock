package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/skills-lock/skil-lock/internal/approvals"
	"github.com/skills-lock/skil-lock/internal/diff"
	"github.com/skills-lock/skil-lock/internal/lockfile"
	"github.com/skills-lock/skil-lock/internal/model"
	"github.com/skills-lock/skil-lock/internal/policy"
	"github.com/skills-lock/skil-lock/internal/sarif"
	"github.com/skills-lock/skil-lock/internal/scan"
)

// Output formats accepted by `skil-lock ci --format`.
const (
	formatMarkdown = "markdown"
	formatSARIF    = "sarif"
)

const (
	defaultPolicyName    = ".skil-lock.yaml"
	defaultApprovalsName = ".skil-lock-approvals.yaml"
)

// errBlocking is returned to main when policy is in block mode and the
// diff contains blocking-severity entries. main exits 1 on any error,
// so this is currently indistinguishable from other failures; the
// stderr verdict line is what tells the user what happened.
var errBlocking = errors.New("policy block: capability deltas require approval")

// blockingThreshold is the severity at or above which an entry fails
// the build under `mode: block`. Medium matches the diff engine's
// default severity for added shell_commands / network_urls, which is
// the wedge: anything new is suspect until approved.
const blockingThreshold = model.SeverityMedium

func newCICmd() *cobra.Command {
	var policyPath, lockPath, approvalsPath, format string
	var prNumber int
	cmd := &cobra.Command{
		Use:   "ci [path]",
		Short: "Verify [path] against its committed skills.lock and .skil-lock.yaml.",
		Long: `ci re-scans [path] (default .), loads .skil-lock.yaml (or falls
back to warn-mode defaults if absent), loads skills.lock, computes the
capability delta, drops deltas pre-approved in .skil-lock-approvals.yaml,
and lifts severities per policy. Exit code is 1 when policy is mode=block
and any remaining delta is at severity >= medium; exit code is 0 otherwise
(warn mode never blocks the build).

This is the command the SkilLock GitHub Action invokes; the same command
works locally — run it before opening a PR to see what reviewers will
see.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}

			switch format {
			case formatMarkdown, formatSARIF:
			default:
				return fmt.Errorf("invalid --format %q (want %q or %q)", format, formatMarkdown, formatSARIF)
			}

			pol, err := loadPolicy(cmd, root, policyPath)
			if err != nil {
				return err
			}

			as, err := loadApprovals(cmd, root, approvalsPath)
			if err != nil {
				return err
			}

			lp := lockPath
			if lp == "" {
				lp = filepath.Join(root, defaultLockfileName)
			}
			baseline, err := lockfile.Load(lp)
			if err != nil {
				return fmt.Errorf("load %s: %w", lp, err)
			}

			rep, err := scan.Repo(root)
			if err != nil {
				return fmt.Errorf("scan %s: %w", root, err)
			}
			for _, e := range rep.Errors {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", e.Path, e.Error)
			}

			current := buildLockfile(rep)
			d := diff.Compare(baseline, current, lp, "<working tree>")

			pr := resolvePR(prNumber)

			d, applied, expired := approvals.Filter(d, as, time.Now(), pr)
			for _, a := range applied {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"approved: skill=%s reviewer=%s reason=%q\n",
					a.Skill, a.Reviewer, a.Reason)
			}
			for _, a := range expired {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"approval expired: skill=%s reviewer=%s expires_at=%s - delta resurfaced\n",
					a.Skill, a.Reviewer, a.ExpiresAt.UTC().Format(time.RFC3339))
			}

			policy.Apply(&d, pol)

			verdict, blocked := decide(d, pol)
			switch format {
			case formatSARIF:
				doc, err := sarif.Render(d, current, version)
				if err != nil {
					return fmt.Errorf("render sarif: %w", err)
				}
				_, _ = cmd.OutOrStdout().Write(doc)
				_, _ = fmt.Fprintln(cmd.OutOrStdout())
			default:
				_, _ = fmt.Fprint(cmd.OutOrStdout(), diff.RenderMarkdownPR(d, verdict, pr))
			}
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), verdict)

			// A skill that fails to parse is not analysable, so its capability
			// surface is unknown. In block mode that is itself a failure: a
			// planted parse error (e.g. a malformed SKILL.md) would otherwise
			// drop the skill from the scan and read as a benign removal,
			// passing CI while a sibling script was rewritten. Refuse to pass.
			if pol.Mode == model.PolicyModeBlock && len(rep.Errors) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"BLOCK: %d skill(s) failed to parse and could not be analysed\n", len(rep.Errors))
				return errBlocking
			}

			if blocked {
				return errBlocking
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "Path to .skil-lock.yaml (default: <path>/.skil-lock.yaml)")
	cmd.Flags().StringVar(&lockPath, "lockfile", "", "Path to skills.lock (default: <path>/skills.lock)")
	cmd.Flags().StringVar(&approvalsPath, "approvals", "", "Path to .skil-lock-approvals.yaml (default: <path>/.skil-lock-approvals.yaml)")
	cmd.Flags().StringVar(&format, "format", formatMarkdown, "Output format: markdown (default; PR comment) or sarif (GitHub Code Scanning)")
	cmd.Flags().IntVar(&prNumber, "pr", 0, "Pull-request number for PR-scoped approvals (auto-detected from GITHUB_EVENT_PATH in GitHub Actions; 0 = no PR context)")
	return cmd
}

// resolvePR returns the pull-request number for approval scoping: the
// --pr flag when given, otherwise the pull_request.number from the
// GitHub Actions event payload when running inside a pull_request
// workflow. 0 means "no PR context" — PR-scoped approvals will not
// match. Auto-detection keeps older action.yml versions working: a
// newer pinned binary picks up PR context without the Action passing
// the flag.
func resolvePR(flagVal int) int {
	if flagVal > 0 {
		return flagVal
	}
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return 0
	}
	raw, err := os.ReadFile(eventPath)
	if err != nil {
		return 0
	}
	var event struct {
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0
	}
	return event.PullRequest.Number
}

// loadPolicy resolves the policy file path and loads it. A missing
// policy file is not an error — it falls back to Default() and emits
// a single-line stderr notice so an absent .skil-lock.yaml is visible
// without being noisy.
func loadPolicy(cmd *cobra.Command, root, override string) (model.Policy, error) {
	pp := override
	if pp == "" {
		pp = filepath.Join(root, defaultPolicyName)
	}
	pol, err := policy.Load(pp)
	switch {
	case errors.Is(err, policy.ErrMissingPolicy):
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"no %s found at %s, using defaults (mode=warn)\n", defaultPolicyName, pp)
		return policy.Default(), nil
	case err != nil:
		return model.Policy{}, fmt.Errorf("load policy: %w", err)
	}
	return pol, nil
}

// loadApprovals resolves the approvals file path and loads it. A missing
// approvals file is not an error — most repos start without any approved
// deltas, and adding the file is itself a workflow step we don't want
// to gate the first run on. Unlike loadPolicy, no stderr notice is
// emitted on absence: the empty case is the common one, and a notice
// per run would be noise.
func loadApprovals(cmd *cobra.Command, root, override string) ([]approvals.Approval, error) {
	ap := override
	if ap == "" {
		ap = filepath.Join(root, defaultApprovalsName)
	}
	as, err := approvals.Load(ap)
	switch {
	case errors.Is(err, approvals.ErrMissingApprovals):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load approvals: %w", err)
	}
	return as, nil
}

// decide returns the human-facing verdict line and whether the build
// should fail. Warn mode never fails the build.
func decide(d model.Diff, pol model.Policy) (verdict string, blocked bool) {
	if len(d.Entries) == 0 {
		return "PASS: no capability deltas", false
	}
	flagging := countAtOrAbove(d, blockingThreshold)
	if pol.Mode == model.PolicyModeBlock && flagging > 0 {
		return fmt.Sprintf("BLOCK: %d of %d entries at severity >= %s",
			flagging, len(d.Entries), blockingThreshold), true
	}
	if pol.Mode == model.PolicyModeBlock {
		return fmt.Sprintf("PASS: %d entries, none at severity >= %s",
			len(d.Entries), blockingThreshold), false
	}
	return fmt.Sprintf("WARN: %d entries (mode=warn, not blocking)", len(d.Entries)), false
}

func countAtOrAbove(d model.Diff, threshold model.Severity) int {
	target := severityRank(threshold)
	n := 0
	for _, e := range d.Entries {
		if severityRank(e.Severity) >= target {
			n++
		}
	}
	return n
}

func severityRank(s model.Severity) int {
	switch s {
	case model.SeverityHigh:
		return 4
	case model.SeverityMedium:
		return 3
	case model.SeverityLow:
		return 2
	case model.SeverityInfo:
		return 1
	}
	return 0
}
