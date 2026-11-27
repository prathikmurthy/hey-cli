package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const athenaGitPullError = `error: Your local changes to the following files would be overwritten by merge:
	apps/desktop/src-tauri/Cargo.toml
Please commit your changes or stash them before you merge.
Aborting`

func TestCollectGitPullOverwriteFixture(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.9999")
	if err := os.WriteFile(outFile, []byte("\x1b[31m"+athenaGitPullError+"\x1b[0m\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HEY_SHELL", "zsh")
	t.Setenv("HEY_HISTORY", "cd ~/Code/athena\ngit status\ngit pull --ff-only")
	t.Setenv("HEY_LAST_CMD", "git pull --ff-only")
	t.Setenv("HEY_LAST_EXIT", "1")
	t.Setenv("HEY_LAST_OUTPUT_FILE", outFile)
	t.Setenv("HEY_LAST_OUTPUT", "")
	// Unset HEY_LAST_OUTPUT so HEY_LAST_OUTPUT_FILE is read.
	os.Unsetenv("HEY_LAST_OUTPUT")

	for _, q := range []string{"what do i do", "how do i fix this"} {
		b := Collect(q, 8, 40)
		if b.LastCommand != "git pull --ff-only" {
			t.Errorf("LastCommand = %q want git pull --ff-only", b.LastCommand)
		}
		if b.LastExitCode == nil || *b.LastExitCode != 1 {
			t.Errorf("LastExitCode = %v want 1", b.LastExitCode)
		}
		if !strings.Contains(b.LastOutput, "apps/desktop/src-tauri/Cargo.toml") {
			t.Errorf("LastOutput missing Cargo.toml: %q", b.LastOutput)
		}
		if strings.Contains(b.LastOutput, "\x1b[") {
			t.Errorf("LastOutput still contains ANSI escapes: %q", b.LastOutput)
		}

		formatted := b.Format()
		for _, want := range []string{
			"shell: zsh",
			"last_command: git pull --ff-only",
			"last_exit: 1",
			"last_output:\nerror: Your local changes to the following files would be overwritten by merge:",
			"apps/desktop/src-tauri/Cargo.toml",
			"q: " + q,
		} {
			if !strings.Contains(formatted, want) {
				t.Errorf("Format() missing %q in:\n%s", want, formatted)
			}
		}
	}
}

func TestCollectRedactsHistoryAndExplicitOutput(t *testing.T) {
	t.Setenv("HEY_HISTORY", "export OPENAI_API_KEY=sk-1234567890abcdefghijklmnop\ngit pull --ff-only")
	t.Setenv("HEY_LAST_CMD", "curl -H 'Authorization: Bearer secret_token_1234567890' https://example.com")
	t.Setenv("HEY_LAST_EXIT", "22")
	t.Setenv("HEY_LAST_OUTPUT", "TOKEN=supersecretvalue\ncurl: (22) The requested URL returned error: 401")

	b := Collect("why did curl fail", 8, 40)
	formatted := b.Format()

	for _, leaked := range []string{"sk-1234567890", "secret_token_1234567890", "supersecretvalue"} {
		if strings.Contains(formatted, leaked) {
			t.Errorf("secret %q leaked into formatted context:\n%s", leaked, formatted)
		}
	}
	if !strings.Contains(formatted, "returned error: 401") {
		t.Errorf("expected curl error preserved:\n%s", formatted)
	}
}

func TestCollectIgnoresStaleFallbackOutput(t *testing.T) {
	home := t.TempDir()
	heyDir := filepath.Join(home, ".hey")
	if err := os.MkdirAll(heyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fallback := filepath.Join(heyDir, "last_output")
	if err := os.WriteFile(fallback, []byte("old stale error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(fallback, stale, stale); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("TMUX", "")
	t.Setenv("WEZTERM_PANE", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("HEY_LAST_OUTPUT_FILE", "")
	os.Unsetenv("HEY_LAST_OUTPUT")

	b := Collect("what do i do", 8, 40)
	if b.LastOutput != "" {
		t.Errorf("expected stale fallback ignored, got %q", b.LastOutput)
	}
}
