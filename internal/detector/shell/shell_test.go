package shell

import (
	"reflect"
	"testing"

	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

func TestDetect_FromBashCodeFence(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "pdftotext input.pdf out.txt\ncurl -sSf https://example.com -o data\n"},
		},
	}
	got := Detect(p)
	want := []string{"curl", "pdftotext"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_HonorsLineContinuation(t *testing.T) {
	// Regression for #12: a multi-line curl joined with `\` must yield a
	// single "curl" command, not curl + the leaked -H / -d flags.
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "curl -X POST \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"event\":\"ship\"}' \\\n  https://api.example.com/notify\n"},
		},
	}
	got := Detect(p)
	want := []string{"curl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_FromBundledScript(t *testing.T) {
	p := claude.ParsedSkill{
		Scripts: []claude.Script{
			{RelPath: "scripts/extract.sh", Content: "#!/usr/bin/env bash\nset -euo pipefail\nfor f in ./*.pdf; do\n  pdftotext \"$f\" out.txt\ndone\n"},
		},
	}
	got := Detect(p)
	want := []string{"pdftotext"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_SkipsBuiltinsAndAssignments(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "FOO=bar\nset -e\necho hi\nif true; then\n  /usr/bin/curl https://example.com\nfi\n"},
		},
	}
	got := Detect(p)
	want := []string{"curl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_PipelineSegments(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "sh", Content: "cat foo.txt | grep bar | wc -l\n"},
		},
	}
	got := Detect(p)
	want := []string{"cat", "grep", "wc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_SequencedCommands(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "git pull && go build ./... ; echo done\n"},
		},
	}
	got := Detect(p)
	want := []string{"git", "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_StripsLeadingSudoAndEnv(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "sudo apt-get install foo\nenv VAR=1 python3 script.py\n"},
		},
	}
	got := Detect(p)
	want := []string{"apt-get", "python3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: want %v, got %v", want, got)
	}
}

func TestDetect_AllowedToolsBashSentinelWhenNoConcreteCommand(t *testing.T) {
	p := claude.ParsedSkill{
		AllowedTools: []string{"Bash", "Read"},
	}
	got := Detect(p)
	want := []string{ShellSentinel}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sentinel: want %v, got %v", want, got)
	}
}

func TestDetect_AllowedToolsBashSuppressedWhenConcreteCommandPresent(t *testing.T) {
	p := claude.ParsedSkill{
		AllowedTools: []string{"Bash"},
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "git status\n"},
		},
	}
	got := Detect(p)
	want := []string{"git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with concrete cmd, sentinel should not appear: %v", got)
	}
}

func TestDetect_NonShellFencesIgnored(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "python", Content: "import os\nos.system('rm -rf /')\n"},
			{Language: "", Content: "literal example block, not language-tagged\n"},
		},
	}
	got := Detect(p)
	if len(got) != 0 {
		t.Errorf("non-shell fences should be ignored; got %v", got)
	}
}

func TestDetect_NonShellScriptsIgnored(t *testing.T) {
	p := claude.ParsedSkill{
		Scripts: []claude.Script{
			{RelPath: "scripts/extract.py", Content: "import os\nos.system('curl http://x')\n"},
		},
	}
	got := Detect(p)
	if len(got) != 0 {
		t.Errorf("non-.sh scripts without shebang should be ignored; got %v", got)
	}
}

func TestDetect_ShebangScriptWithoutShExtension(t *testing.T) {
	p := claude.ParsedSkill{
		Scripts: []claude.Script{
			{RelPath: "scripts/extract", Content: "#!/usr/bin/env bash\ngit status\n"},
		},
	}
	got := Detect(p)
	want := []string{"git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shebang detection: want %v, got %v", want, got)
	}
}

func TestDetect_DeduplicatesAcrossSources(t *testing.T) {
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "curl https://example.com\n"},
		},
		Scripts: []claude.Script{
			{RelPath: "scripts/x.sh", Content: "curl https://example.com\n"},
		},
	}
	got := Detect(p)
	want := []string{"curl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}
