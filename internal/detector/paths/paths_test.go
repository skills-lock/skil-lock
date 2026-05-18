package paths

import (
	"reflect"
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
