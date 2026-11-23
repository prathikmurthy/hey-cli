package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellSnippetIncludesPathAndSource(t *testing.T) {
	s := ShellSnippet("zsh", "/home/u/.hey/shell", "/home/u/.local/bin")
	if !strings.Contains(s, beginMarker) || !strings.Contains(s, endMarker) {
		t.Fatalf("missing markers: %q", s)
	}
	if !strings.Contains(s, `export PATH="/home/u/.local/bin":$PATH`) {
		t.Fatalf("missing PATH export: %q", s)
	}
	if !strings.Contains(s, `source "/home/u/.hey/shell/hey.zsh"`) {
		t.Fatalf("missing source: %q", s)
	}
}

func TestUpsertRCIdempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	snippet := ShellSnippet("zsh", filepath.Join(dir, "shell"), filepath.Join(dir, "bin"))
	if err := upsertRC(rc, snippet); err != nil {
		t.Fatal(err)
	}
	if err := upsertRC(rc, snippet); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(data), beginMarker); c != 1 {
		t.Fatalf("expected 1 begin marker, got %d in %s", c, data)
	}
}
