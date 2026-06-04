package network

import (
	"reflect"
	"testing"

	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

func TestDetect_PicksUpUrlsAcrossSources(t *testing.T) {
	p := claude.ParsedSkill{
		Body: "See https://api.example.com/docs for details.\nAlso http://internal.local:8080/health.",
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "curl https://github.com/x/y\n"},
		},
		Scripts: []claude.Script{
			{RelPath: "scripts/fetch.sh", Content: "wget https://example.com/data.tar.gz\n"},
		},
	}
	got := Detect(p)
	want := []string{
		"http://internal.local:8080/health",
		"https://api.example.com/docs",
		"https://example.com/data.tar.gz",
		"https://github.com/x/y",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v\ngot  %v", want, got)
	}
}

func TestDetect_UrlOnContinuationLine(t *testing.T) {
	// Regression guard for #12: the trailing URL of a multi-line curl
	// must still be extracted.
	p := claude.ParsedSkill{
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "curl -X POST \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"event\":\"ship\"}' \\\n  https://api.example.com/notify\n"},
		},
	}
	got := Detect(p)
	want := []string{"https://api.example.com/notify"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestDetect_StripsTrailingPunctuation(t *testing.T) {
	p := claude.ParsedSkill{
		Body: "Look here: https://api.example.com/v1/posts. End.",
	}
	got := Detect(p)
	want := []string{"https://api.example.com/v1/posts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trailing period not stripped: %v", got)
	}
}

func TestDetect_AcceptsGlobHostsInAllowlistStyle(t *testing.T) {
	p := claude.ParsedSkill{
		Body: "Allow https://*.github.com and https://api.openai.com.",
	}
	got := Detect(p)
	want := []string{"https://*.github.com", "https://api.openai.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("glob host: want %v, got %v", want, got)
	}
}

func TestDetect_Deduplicates(t *testing.T) {
	p := claude.ParsedSkill{
		Body: "https://example.com https://example.com",
		CodeBlocks: []claude.CodeBlock{
			{Language: "bash", Content: "curl https://example.com\n"},
		},
	}
	got := Detect(p)
	want := []string{"https://example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedupe: %v", got)
	}
}

func TestDetect_NoURLsReturnsEmpty(t *testing.T) {
	p := claude.ParsedSkill{Body: "Plain prose, no links."}
	got := Detect(p)
	if len(got) != 0 {
		t.Errorf("expected empty; got %v", got)
	}
}
