package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

func TestDetect_StdoutRedirectIsWrite(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "echo hello > ./output/log.txt\n"},
		},
	}
	got := Detect(p)
	if !reflect.DeepEqual(got.Writes, []string{"./output/log.txt"}) {
		t.Errorf("writes: %v", got.Writes)
	}
	if len(got.Reads) != 0 {
		t.Errorf("reads should be empty: %v", got.Reads)
	}
}

func TestDetect_AppendRedirectIsWrite(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "date >> ./output/log.txt\n"},
		},
	}
	got := Detect(p)
	if !reflect.DeepEqual(got.Writes, []string{"./output/log.txt"}) {
		t.Errorf("writes: %v", got.Writes)
	}
}

func TestDetect_StdinRedirectIsRead(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "wc -l < ./input/data.txt\n"},
		},
	}
	got := Detect(p)
	if !reflect.DeepEqual(got.Reads, []string{"./input/data.txt"}) {
		t.Errorf("reads: %v", got.Reads)
	}
}

func TestDetect_DotfileSecretReadsAreSurfaced(t *testing.T) {
	// Regression for #10: dot-prefix files without an extension (.env,
	// .npmrc) are the top secret-exfil targets but were filtered out as
	// flag/hidden-dir noise, leaving file_reads empty.
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat .env\ncat .npmrc\ncat .env.local\n"},
		},
	}
	got := Detect(p)
	wantReads := []string{".env", ".env.local", ".npmrc"}
	if !reflect.DeepEqual(got.Reads, wantReads) {
		t.Errorf("reads: want %v, got %v", wantReads, got.Reads)
	}
}

func TestDetect_CurrentAndParentDirAreNotPaths(t *testing.T) {
	// "." and ".." must NOT be treated as dotfile reads.
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat .\nls ..\n"},
		},
	}
	got := Detect(p)
	if len(got.Reads) != 0 {
		t.Errorf("reads should be empty for . and ..: %v", got.Reads)
	}
}

func TestDetect_ReadOnlyCommandArgsAreReads(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat ./.env\ngrep -F secret ./secrets/keys.txt\n"},
		},
	}
	got := Detect(p)
	wantReads := []string{"./.env", "./secrets/keys.txt"}
	if !reflect.DeepEqual(got.Reads, wantReads) {
		t.Errorf("reads: want %v, got %v", wantReads, got.Reads)
	}
}

func TestDetect_WriteCommandArgsAreWrites(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "touch ./build/.stamp\nrm ./tmp/*.bak\n"},
		},
	}
	got := Detect(p)
	wantWrites := []string{"./build/.stamp", "./tmp/*.bak"}
	if !reflect.DeepEqual(got.Writes, wantWrites) {
		t.Errorf("writes: want %v, got %v", wantWrites, got.Writes)
	}
}

func TestDetect_FlagsAreNotPaths(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "grep -F -i pattern ./input/file.txt\n"},
		},
	}
	got := Detect(p)
	wantReads := []string{"./input/file.txt"}
	if !reflect.DeepEqual(got.Reads, wantReads) {
		t.Errorf("reads: want %v, got %v", wantReads, got.Reads)
	}
}

func TestDetect_GlobsPreservedVerbatim(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat ./input/*.pdf\n"},
		},
	}
	got := Detect(p)
	wantReads := []string{"./input/*.pdf"}
	if !reflect.DeepEqual(got.Reads, wantReads) {
		t.Errorf("globs: want %v, got %v", wantReads, got.Reads)
	}
}

func TestDetect_StripsQuotes(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat \"./path with spaces/file.txt\"\n"},
		},
	}
	got := Detect(p)
	wantReads := []string{"./path with spaces/file.txt"}
	if !reflect.DeepEqual(got.Reads, wantReads) {
		t.Errorf("quotes: want %v, got %v", wantReads, got.Reads)
	}
}

func TestDetect_HttpUrlsExcluded(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "curl https://example.com/data.json > ./out.json\n"},
		},
	}
	got := Detect(p)
	// curl is not in readOnly/write maps → its non-flag arg is classified
	// as a read by default; URL must be filtered out, leaving the
	// redirect target as the only write.
	if !reflect.DeepEqual(got.Writes, []string{"./out.json"}) {
		t.Errorf("writes: %v", got.Writes)
	}
	for _, r := range got.Reads {
		if r == "https://example.com/data.json" {
			t.Errorf("URL should not appear in reads: %v", got.Reads)
		}
	}
}

func TestDetect_ShellShebangScriptScanned(t *testing.T) {
	p := claude.ParsedSkill{
		Scripts: []claude.Script{
			{RelPath: "scripts/install", Content: "#!/usr/bin/env bash\ntouch ./.lock\n"},
		},
	}
	got := Detect(p)
	if !reflect.DeepEqual(got.Writes, []string{"./.lock"}) {
		t.Errorf("shebang script writes: %v", got.Writes)
	}
}

func TestDetect_NonShellScriptIgnored(t *testing.T) {
	p := claude.ParsedSkill{
		Scripts: []claude.Script{
			{RelPath: "scripts/x.py", Content: "open('./.env').read()\n"},
		},
	}
	got := Detect(p)
	if len(got.Reads) != 0 || len(got.Writes) != 0 {
		t.Errorf(".py without shebang should be ignored: %+v", got)
	}
}

func TestDetect_BareWordIsNotPath(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat name\n"},
		},
	}
	got := Detect(p)
	if len(got.Reads) != 0 {
		t.Errorf("bare word should not be a path: %v", got.Reads)
	}
}

func TestDetect_DedupesAcrossLines(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "cat ./.env\ngrep x ./.env\n"},
		},
	}
	got := Detect(p)
	if !reflect.DeepEqual(got.Reads, []string{"./.env"}) {
		t.Errorf("dedupe: %v", got.Reads)
	}
}

// TestSpecStatesTokenBound pins the token bound stated in SPEC.md prose
// to the constant the detector actually applies.
//
// The pointer emitted as run.properties.interpretationUri resolves to
// SPEC.md §14.5, and a companion test proves the anchor resolves. Nothing
// proved the prose behind it was true. This closes the part of that gap
// that is mechanically decidable: the enumerated bounds include one
// constant, and a constant in prose can be held to the constant in code.
// It declares nothing about coverage — it asserts only that two values
// agree — so it cannot overstate what the tool checks.
//
// What it does not reach: whether the four bounds listed in §14.5 are all
// of them. That residue is undeclarable by construction, and a test
// claiming otherwise would be the coverage claim this design refuses.
func TestSpecStatesTokenBound(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "..", "SPEC.md"))
	if err != nil {
		t.Fatalf("read SPEC.md: %v", err)
	}

	stated := tokenBoundsStatedIn(string(spec))

	// Vacuity guard. Without it, deleting the sentence — or rewording it
	// so the extractor stops matching — turns this into a test that
	// passes over an empty set. A constraint that holds because nothing
	// was found is the failure this thread keeps turning up: it looks
	// green on precisely the input it should be judging.
	if len(stated) == 0 {
		t.Fatal("SPEC.md states no token byte bound — §14.5 must state the bound the detector applies, " +
			"or interpretationUri points at a section that has stopped describing the tool")
	}

	for _, got := range stated {
		if got != MaxTokenBytes {
			t.Errorf("SPEC.md states a %d-byte token bound, detector applies %d — "+
				"the interpretationUri target describes a tool this is not", got, MaxTokenBytes)
		}
	}
}

// TestTokenBoundExtractorCatchesDivergence is the negative control for
// the test above. A check that has only ever been observed to pass is not
// known to be capable of failing, so the extractor is run over prose
// stating a bound that is deliberately wrong.
func TestTokenBoundExtractorCatchesDivergence(t *testing.T) {
	const wrong = "Tokens over 8192 bytes are not considered as paths."

	stated := tokenBoundsStatedIn(wrong)
	if len(stated) != 1 {
		t.Fatalf("extractor found %d bounds in the control prose, want 1 — "+
			"it cannot catch a divergence it cannot see", len(stated))
	}
	if stated[0] == MaxTokenBytes {
		t.Fatalf("control prose states %d, which matches the constant — the control proves nothing", stated[0])
	}
}

// tokenBoundsStatedIn returns every byte figure SPEC.md states about
// token length. Sentences are the unit rather than lines, so rewrapping
// the prose does not silently empty the result set: a bound reworded
// across a line break still reads as one sentence here.
func tokenBoundsStatedIn(text string) []int {
	collapsed := strings.Join(strings.Fields(text), " ")

	var bounds []int
	for _, sentence := range sentenceSplit.Split(collapsed, -1) {
		if !strings.Contains(strings.ToLower(sentence), "token") {
			continue
		}
		for _, m := range byteFigure.FindAllStringSubmatch(sentence, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			bounds = append(bounds, n)
		}
	}
	return bounds
}

// A sentence ends at a period followed by whitespace or end of text. The
// section references (§14.5) embed a period followed by a digit, so they
// do not split.
var sentenceSplit = regexp.MustCompile(`\.(?:\s+|$)`)

var byteFigure = regexp.MustCompile(`(\d+)[ -]bytes?\b`)
