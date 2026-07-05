package main

import (
	"strings"
	"testing"
)

func TestExtractGoFiles(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single file with line",
			input: `db/sqlite.go:42: no such column: room_code`,
			want:  []string{"db/sqlite.go"},
		},
		{
			name: "multiple files",
			input: `server/types.go:105: undefined: Bet
server/server.go:1021: cannot use Bet{}`,
			want: []string{"server/types.go", "server/server.go"},
		},
		{
			name:  "with directories",
			input: `pkg/mod/cache/download/example.go:1 AND server/utils.go:55`,
			want:  []string{"pkg/mod/cache/download/example.go", "server/utils.go"},
		},
		{
			name:  "no go files",
			input: `panic: runtime error: index out of range [0]`,
			want:  nil,
		},
		{
			name:  "deduplicates",
			input: "db/sqlite.go:42: error\ndb/sqlite.go:55: error",
			want:  []string{"db/sqlite.go"},
		},
		{
			name:  "non-go file ignored",
			input: `Dockerfile:5: error`,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoFiles(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("extractGoFiles() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractGoFiles()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	errorText := "db/sqlite.go:42: no such column: room_code"
	snippets := "=== db/sqlite.go ===\npackage db\n// ..."
	prompt := buildPrompt(errorText, snippets)

	// Must contain key sections
	if !strings.Contains(prompt, "Go backend engineer") {
		t.Error("prompt missing system role context")
	}
	if !strings.Contains(prompt, "unified diff") {
		t.Error("prompt missing output format instruction")
	}
	if !strings.Contains(prompt, errorText) {
		t.Error("prompt missing error text")
	}
	if !strings.Contains(prompt, snippets) {
		t.Error("prompt missing source snippets")
	}
}

func TestBuildPromptNoSnippets(t *testing.T) {
	prompt := buildPrompt("panic: nil pointer dereference", "")
	if !strings.Contains(prompt, "panic: nil pointer dereference") {
		t.Error("prompt missing error text when no snippets")
	}
}
