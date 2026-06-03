package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkMLnop/binGO-CLI/shared"
)

func writeBuzzwordCSV(t *testing.T, path string, rows int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < rows; i++ {
		_, _ = b.WriteString(fmt.Sprintf("buzzword-%d\n", i+1))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("failed to write buzzword csv: %v", err)
	}
}

func TestShowMainMenuHostSkipBuzzwords(t *testing.T) {
	choice, code, buzzwords, err := showMainMenuFromReader("localhost:8080", strings.NewReader("1\n\n"), shared.LoadBuzzwords)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != "host" {
		t.Fatalf("choice = %q, want host", choice)
	}
	if code != "" {
		t.Fatalf("code = %q, want empty", code)
	}
	if buzzwords != nil {
		t.Fatalf("buzzwords = %v, want nil", buzzwords)
	}
}

func TestShowMainMenuJoinWithCode(t *testing.T) {
	choice, code, buzzwords, err := showMainMenuFromReader("localhost:8080", strings.NewReader("2\nBINGO-ABCDE\n"), shared.LoadBuzzwords)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != "join" {
		t.Fatalf("choice = %q, want join", choice)
	}
	if code != "BINGO-ABCDE" {
		t.Fatalf("code = %q, want BINGO-ABCDE", code)
	}
	if buzzwords != nil {
		t.Fatalf("buzzwords = %v, want nil", buzzwords)
	}
}

func TestShowMainMenuJoinEmptyCode(t *testing.T) {
	_, _, _, err := showMainMenuFromReader("localhost:8080", strings.NewReader("2\n\n"), shared.LoadBuzzwords)
	if err == nil {
		t.Fatal("expected error for empty game code")
	}
	if !strings.Contains(err.Error(), "game code cannot be empty") {
		t.Fatalf("error = %q, want game code cannot be empty", err.Error())
	}
}

func TestShowMainMenuInvalidSelection(t *testing.T) {
	_, _, _, err := showMainMenuFromReader("localhost:8080", strings.NewReader("3\n"), shared.LoadBuzzwords)
	if err == nil {
		t.Fatal("expected error for invalid selection")
	}
	if !strings.Contains(err.Error(), `invalid selection "3"`) {
		t.Fatalf("error = %q, want invalid selection", err.Error())
	}
}

func TestShowMainMenuHostWithValidCSV(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "buzzwords.csv")
	writeBuzzwordCSV(t, csvPath, 9)

	input := fmt.Sprintf("1\n%s\n", csvPath)
	choice, code, buzzwords, err := showMainMenuFromReader("localhost:8080", strings.NewReader(input), shared.LoadBuzzwords)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != "host" {
		t.Fatalf("choice = %q, want host", choice)
	}
	if code != "" {
		t.Fatalf("code = %q, want empty", code)
	}
	if len(buzzwords) != 9 {
		t.Fatalf("len(buzzwords) = %d, want 9", len(buzzwords))
	}
}

func TestShowMainMenuHostWithMissingCSV(t *testing.T) {
	input := "1\n/does/not/exist.csv\n"
	_, _, _, err := showMainMenuFromReader("localhost:8080", strings.NewReader(input), shared.LoadBuzzwords)
	if err == nil {
		t.Fatal("expected error for missing buzzword file")
	}
	if !strings.Contains(err.Error(), "failed to load buzzword CSV") {
		t.Fatalf("error = %q, want failed to load buzzword CSV", err.Error())
	}
}

func TestShowMainMenuHostWithInsufficientRows(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "buzzwords-short.csv")
	writeBuzzwordCSV(t, csvPath, 8)

	input := fmt.Sprintf("1\n%s\n", csvPath)
	_, _, _, err := showMainMenuFromReader("localhost:8080", strings.NewReader(input), shared.LoadBuzzwords)
	if err == nil {
		t.Fatal("expected error for insufficient rows")
	}
	if !strings.Contains(err.Error(), "failed to load buzzword CSV") {
		t.Fatalf("error = %q, want failed to load buzzword CSV", err.Error())
	}
}
