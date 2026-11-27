package shell

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashHookCapturesGitPullOverwriteError(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build hey binary into temp binDir.
	heyBin := filepath.Join(binDir, "hey")
	build := exec.Command("go", "build", "-o", heyBin, "../cmd/hey")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build hey: %v\n%s", err, out)
	}

	// Fake agent binary that logs received prompts and returns a concrete git stash command.
	promptsLog := filepath.Join(tmp, "prompts.log")
	agentScript := `#!/usr/bin/env bash
for last; do true; done
printf '%s\n===PROMPT_SEP===\n' "$last" >> "$HEY_TEST_PROMPTS"
echo "COMMAND: git stash push -- apps/desktop/src-tauri/Cargo.toml && git pull --ff-only && git stash pop"
echo "NOTE: Stashes Cargo.toml, pulls cleanly, then reapplies your edit."
`
	if err := os.WriteFile(filepath.Join(binDir, "agent"), []byte(agentScript), 0o755); err != nil {
		t.Fatal(err)
	}

	// Set up bare remote + local repo reproducing the exact athena Cargo.toml merge error.
	remoteDir := filepath.Join(tmp, "remote.git")
	athenaDir := filepath.Join(tmp, "athena")
	otherDir := filepath.Join(tmp, "other")
	mustRun(t, tmp, "git", "init", "-q", "--bare", remoteDir)
	mustRun(t, tmp, "git", "clone", "-q", remoteDir, athenaDir)
	mustRun(t, athenaDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, athenaDir, "git", "config", "user.name", "Test")

	cargoRel := filepath.Join("apps", "desktop", "src-tauri", "Cargo.toml")
	cargoPath := filepath.Join(athenaDir, cargoRel)
	if err := os.MkdirAll(filepath.Dir(cargoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cargoPath, []byte("[package]\nname = \"athena\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, athenaDir, "git", "add", ".")
	mustRun(t, athenaDir, "git", "commit", "-q", "-m", "init")
	mustRun(t, athenaDir, "git", "push", "-q", "-u", "origin", "HEAD")

	// Push upstream commit modifying Cargo.toml.
	mustRun(t, tmp, "git", "clone", "-q", remoteDir, otherDir)
	mustRun(t, otherDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, otherDir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(otherDir, cargoRel), []byte("[package]\nname = \"athena\"\nversion = \"0.2.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, otherDir, "git", "commit", "-aq", "-m", "bump version")
	mustRun(t, otherDir, "git", "push", "-q")

	// Make local uncommitted edits to Cargo.toml and add a .env secret file.
	if err := os.WriteFile(cargoPath, []byte("[package]\nname = \"athena\"\nversion = \"0.1.0-local\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const secretVal = "sk-nevercapturethissecret1234567890"
	if err := os.WriteFile(filepath.Join(athenaDir, ".env"), []byte("API_KEY="+secretVal+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	heyBashPath, err := filepath.Abs("hey.bash")
	if err != nil {
		t.Fatal(err)
	}

	script := strings.Join([]string{
		"source " + heyBashPath,
		"git pull --ff-only",
		"hey --print what do i do",
		"hey --print how do i fix this",
		"cat .env",
		"hey --print after env",
		"exit",
	}, "\n") + "\n"

	cmd := exec.Command("bash", "--norc", "--noprofile", "-i")
	cmd.Dir = athenaDir
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = []string{
		"HOME=" + filepath.Join(tmp, "home"),
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"TERM=dumb",
		"HEY_CAPTURE_FORCE=1",
		"HEY_TEST_PROMPTS=" + promptsLog,
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("interactive bash session failed: %v\n%s", err, out.String())
	}

	rawPrompts, err := os.ReadFile(promptsLog)
	if err != nil {
		t.Fatalf("read prompts log: %v\nshell output:\n%s", err, out.String())
	}
	prompts := strings.Split(strings.TrimSpace(string(rawPrompts)), "===PROMPT_SEP===")
	var nonEmpty []string
	for _, p := range prompts {
		if s := strings.TrimSpace(p); s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	if len(nonEmpty) != 3 {
		t.Fatalf("expected 3 agent prompts, got %d:\n%s", len(nonEmpty), string(rawPrompts))
	}

	for i, p := range nonEmpty {
		for _, want := range []string{
			"last_command: git pull --ff-only",
			"last_exit: 1",
			"Your local changes to the following files would be overwritten by merge:",
			"apps/desktop/src-tauri/Cargo.toml",
		} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt #%d missing %q:\n%s", i+1, want, p)
			}
		}
		if strings.Contains(p, secretVal) {
			t.Errorf("prompt #%d leaked .env secret!", i+1)
		}
	}
}

func TestBashShouldCaptureFilters(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	heyBashPath, err := filepath.Abs("hey.bash")
	if err != nil {
		t.Fatal(err)
	}
	// Stub `hey` on PATH so hey.bash loads its functions.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "hey"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		cmd     string
		capture bool
	}{
		{"git pull --ff-only", true},
		{"git push origin main", true},
		{"git status", true},
		{"git commit -m 'fix'", true},
		{"cargo build", true},
		{"go test ./...", true},
		{"python3 app.py", true},
		{"hey what do i do", false},
		{"vim apps/desktop/src-tauri/Cargo.toml", false},
		{"less README.md", false},
		{"ls -la", false},
		{"cd ..", false},
		{"cat .env", false},
		{"cat .env.local", false},
		{"env", false},
		{"printenv", false},
		{"export SECRET_KEY=abc", false},
		{"git diff", false},
		{"git log", false},
		{"git commit", false},
		{"git rebase -i HEAD~3", false},
		{"python3", false},
		{"sleep 10 &", false},
	}

	var script strings.Builder
	script.WriteString("source " + heyBashPath + "\n")
	for _, c := range cases {
		script.WriteString("if _hey_should_capture ")
		script.WriteString(shellQuote(c.cmd))
		script.WriteString("; then echo 'YES'; else echo 'NO'; fi\n")
	}

	cmd := exec.Command("bash", "--norc", "--noprofile")
	cmd.Stdin = strings.NewReader(script.String())
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HEY_CAPTURE_FORCE=1",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash filter test failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d lines want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		want := "NO"
		if c.capture {
			want = "YES"
		}
		if strings.TrimSpace(lines[i]) != want {
			t.Errorf("_hey_should_capture(%q) = %q, want %q", c.cmd, lines[i], want)
		}
	}
}

func TestEmbeddedZshAndBashWidgetsPresent(t *testing.T) {
	zshBytes, err := Files.ReadFile("hey.zsh")
	if err != nil {
		t.Fatalf("read embedded hey.zsh: %v", err)
	}
	zsh := string(zshBytes)
	for _, want := range []string{"_hey_preexec", "_hey_precmd", "_hey_should_capture", "_hey_zshexit", "HEY_LAST_OUTPUT_FILE", "print -z"} {
		if !strings.Contains(zsh, want) {
			t.Errorf("hey.zsh missing %q", want)
		}
	}

	bashBytes, err := Files.ReadFile("hey.bash")
	if err != nil {
		t.Fatalf("read embedded hey.bash: %v", err)
	}
	bash := string(bashBytes)
	for _, want := range []string{"_hey_debug_trap", "_hey_precmd", "_hey_should_capture", "HEY_LAST_OUTPUT_FILE", "read -e -i"} {
		if !strings.Contains(bash, want) {
			t.Errorf("hey.bash missing %q", want)
		}
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
