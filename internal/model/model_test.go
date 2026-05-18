package model

import (
	"testing"
	"time"
)

func TestNewLockfile_InitialState(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 14, 8, 0, time.UTC)
	lf := NewLockfile("skil-lock 0.1.0", now)

	if lf.SchemaVersion != SchemaVersionV01 {
		t.Errorf("schema_version: want %q, got %q", SchemaVersionV01, lf.SchemaVersion)
	}
	if lf.GeneratedBy != "skil-lock 0.1.0" {
		t.Errorf("generated_by: want %q, got %q", "skil-lock 0.1.0", lf.GeneratedBy)
	}
	if !lf.GeneratedAt.Equal(now) {
		t.Errorf("generated_at: want %v, got %v", now, lf.GeneratedAt)
	}
	if lf.Skills == nil {
		t.Error("Skills map must be initialised (non-nil), so callers can assign directly")
	}
	if len(lf.Skills) != 0 {
		t.Errorf("Skills len: want 0, got %d", len(lf.Skills))
	}
}

func TestNewLockfile_NormalisesToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("tz data unavailable: %v", err)
	}
	local := time.Date(2026, 5, 16, 9, 14, 8, 0, loc)
	lf := NewLockfile("skil-lock", local)
	if lf.GeneratedAt.Location() != time.UTC {
		t.Errorf("generated_at must be normalised to UTC; got %v", lf.GeneratedAt.Location())
	}
	if !lf.GeneratedAt.Equal(local) {
		t.Errorf("UTC normalisation should preserve instant; want %v, got %v", local.UTC(), lf.GeneratedAt)
	}
}

func TestDiff_HasBlocking(t *testing.T) {
	cases := []struct {
		name     string
		entries  []DiffEntry
		thresh   Severity
		expected bool
	}{
		{
			name:     "no entries -> not blocking",
			entries:  nil,
			thresh:   SeverityMedium,
			expected: false,
		},
		{
			name:     "info only, threshold medium -> not blocking",
			entries:  []DiffEntry{{Severity: SeverityInfo}},
			thresh:   SeverityMedium,
			expected: false,
		},
		{
			name:     "low only, threshold medium -> not blocking",
			entries:  []DiffEntry{{Severity: SeverityLow}},
			thresh:   SeverityMedium,
			expected: false,
		},
		{
			name:     "exactly at threshold -> blocking",
			entries:  []DiffEntry{{Severity: SeverityMedium}},
			thresh:   SeverityMedium,
			expected: true,
		},
		{
			name:     "above threshold -> blocking",
			entries:  []DiffEntry{{Severity: SeverityHigh}, {Severity: SeverityInfo}},
			thresh:   SeverityMedium,
			expected: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Diff{Entries: tc.entries}
			if got := d.HasBlocking(tc.thresh); got != tc.expected {
				t.Errorf("HasBlocking(%q) = %v; want %v", tc.thresh, got, tc.expected)
			}
		})
	}
}

func TestRuntime_Constants(t *testing.T) {
	if RuntimeClaude == RuntimeCodex {
		t.Fatal("runtime constants must be distinct")
	}
	if string(RuntimeClaude) != "claude" {
		t.Errorf("RuntimeClaude wire value: want %q, got %q", "claude", RuntimeClaude)
	}
	if string(RuntimeCodex) != "codex" {
		t.Errorf("RuntimeCodex wire value: want %q, got %q", "codex", RuntimeCodex)
	}
}
