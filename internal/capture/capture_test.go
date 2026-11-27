package capture

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const gitOverwriteFixture = "\x1b[31merror:\x1b[0m Your local changes to the following files would be overwritten by merge:\n" +
	"\tapps/desktop/src-tauri/Cargo.toml\n" +
	"Please commit your changes or stash them before you merge.\n" +
	"Aborting\n"

func TestSanitizeGitOverwriteFixture(t *testing.T) {
	got := Sanitize(gitOverwriteFixture, 40, 4096)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("expected ANSI stripped, got %q", got)
	}
	wantSubstrings := []string{
		"error: Your local changes to the following files would be overwritten by merge:",
		"apps/desktop/src-tauri/Cargo.toml",
		"Please commit your changes or stash them before you merge.",
		"Aborting",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("Sanitize missing %q in:\n%s", want, got)
		}
	}
}

func TestStripANSIAndCarriageReturns(t *testing.T) {
	in := "\x1b]0;window title\x07\x1b[1;32mBuilding\x1b[0m\n" +
		"Downloading 10%\rDownloading 50%\rDownloading 100%\n" +
		"done\r\n"
	got := StripANSI(in)
	want := "Building\nDownloading 100%\ndone\n"
	if got != want {
		t.Errorf("StripANSI =\n%q\nwant:\n%q", got, want)
	}
}

func TestSanitizeRedactsSecretsAndEnvDumps(t *testing.T) {
	t.Run("inline and assignment secrets", func(t *testing.T) {
		in := strings.Join([]string{
			"OPENAI_API_KEY=sk-1234567890abcdefghijklmnop",
			"GITHUB_TOKEN=ghp_1234567890abcdefghij1234567890",
			"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmnop",
			"curl https://alice:supersecret@api.example.com/v1/data",
			"aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
			"error: connection refused on port 5432",
		}, "\n")

		got := Sanitize(in, 40, 4096)
		for _, leaked := range []string{
			"sk-1234567890",
			"ghp_1234567890",
			"eyJhbGciOiJIUzI1NiJ9",
			"supersecret",
			"AKIAIOSFODNN7EXAMPLE",
		} {
			if strings.Contains(got, leaked) {
				t.Errorf("secret %q leaked into sanitized output:\n%s", leaked, got)
			}
		}
		if !strings.Contains(got, "error: connection refused on port 5432") {
			t.Errorf("expected normal error line preserved, got:\n%s", got)
		}
	})

	t.Run("raw env dump dropped", func(t *testing.T) {
		envDump := strings.Join([]string{
			"HOME=/Users/me",
			"PATH=/usr/local/bin:/usr/bin",
			"SHELL=/bin/zsh",
			"USER=me",
			"CURSOR_API_KEY=sk-secretvalue1234567890",
			"DATABASE_URL=postgres://u:p@host/db",
			"TERM=xterm-256color",
		}, "\n")
		if got := Sanitize(envDump, 40, 4096); got != "" {
			t.Errorf("expected raw env dump to be dropped, got %q", got)
		}
	})

	t.Run("pem private key redacted", func(t *testing.T) {
		in := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA12345\n-----END RSA PRIVATE KEY-----\nafter\n"
		got := Sanitize(in, 40, 4096)
		if strings.Contains(got, "MIIEpAIBAAKCAQEA12345") {
			t.Fatalf("private key body leaked: %s", got)
		}
		if !strings.Contains(got, "[REDACTED PRIVATE KEY]") {
			t.Fatalf("expected redaction marker, got: %s", got)
		}
	})
}

func TestSanitizeTailLinesAndBytes(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %03d", i))
	}
	got := Sanitize(strings.Join(lines, "\n"), 40, 4096)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 40 {
		t.Fatalf("expected 40 lines, got %d", len(gotLines))
	}
	if gotLines[0] != "line 061" || gotLines[39] != "line 100" {
		t.Errorf("unexpected tail window: first=%q last=%q", gotLines[0], gotLines[39])
	}

	// Byte cap preserves tail lines.
	gotSmall := Sanitize(strings.Join(lines, "\n"), 40, 50)
	if len(gotSmall) > 50 {
		t.Errorf("expected <= 50 bytes, got %d: %q", len(gotSmall), gotSmall)
	}
	if !strings.HasSuffix(gotSmall, "line 100") {
		t.Errorf("expected tail preserved, got %q", gotSmall)
	}
}

func TestTeeStreamsAndCapsTail(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "run", "out.123")
	fallbackFile := filepath.Join(dir, "last_output")
	doneFile := filepath.Join(dir, "run", "out.123.done")

	input := strings.Repeat("A", 500) + gitOverwriteFixture
	var passthrough bytes.Buffer

	if err := Tee(strings.NewReader(input), &passthrough, outFile, fallbackFile, doneFile, 256); err != nil {
		t.Fatalf("Tee failed: %v", err)
	}

	// Passthrough stdout must receive 100% of input unmodified.
	if passthrough.String() != input {
		t.Fatalf("passthrough mismatch: got %d bytes want %d", passthrough.Len(), len(input))
	}

	// Captured file is capped at 256 bytes and retains the tail (the git error).
	captured, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read outFile: %v", err)
	}
	if len(captured) > 256 {
		t.Fatalf("captured exceeded maxBytes: %d", len(captured))
	}
	if !strings.Contains(string(captured), "apps/desktop/src-tauri/Cargo.toml") {
		t.Fatalf("captured tail missing git error: %q", string(captured))
	}

	// File permissions must be 0600.
	fi, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("outFile mode = %o, want 0600", perm)
	}

	// Fallback and done marker must exist.
	fb, err := os.ReadFile(fallbackFile)
	if err != nil || string(fb) != string(captured) {
		t.Errorf("fallback mismatch: err=%v len=%d", err, len(fb))
	}
	if _, err := os.Stat(doneFile); err != nil {
		t.Errorf("done marker missing: %v", err)
	}
}

func TestTrimTrailingHeyPrompt(t *testing.T) {
	pane := strings.Join([]string{
		"~/Code/athena % git pull --ff-only",
		"error: Your local changes to the following files would be overwritten by merge:",
		"\tapps/desktop/src-tauri/Cargo.toml",
		"Please commit your changes or stash them before you merge.",
		"Aborting",
		"~/Code/athena % hey what do i do",
		"",
		"",
	}, "\n")

	got := TrimTrailingHeyPrompt(pane)
	if strings.Contains(got, "hey what do i do") {
		t.Errorf("expected trailing hey invocation trimmed, got:\n%s", got)
	}
	if !strings.HasSuffix(got, "Aborting") {
		t.Errorf("expected git error at tail, got:\n%s", got)
	}
}

func TestPruneStale(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "out.old")
	freshFile := filepath.Join(dir, "out.fresh")
	if err := os.WriteFile(oldFile, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshFile, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	PruneStale(dir, 24*time.Hour)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("expected stale file removed, err=%v", err)
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Errorf("expected fresh file kept, err=%v", err)
	}
}
