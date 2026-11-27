package prompt

import (
	"strings"
	"testing"
)

func TestGitOverwriteFixturePromptAndParse(t *testing.T) {
	ctxBlock := strings.Join([]string{
		"cwd: /Users/me/Code/athena",
		"shell: zsh",
		"history:",
		"- git pull --ff-only",
		"last_command: git pull --ff-only",
		"last_exit: 1",
		"last_output:",
		"error: Your local changes to the following files would be overwritten by merge:",
		"\tapps/desktop/src-tauri/Cargo.toml",
		"Please commit your changes or stash them before you merge.",
		"Aborting",
		"q: what do i do",
	}, "\n") + "\n"

	gotPrompt := ForSuggest(ctxBlock)
	for _, want := range []string{
		"last_output contains an error",
		"direct, command-first fix",
		"local changes",
		"git pull --ff-only",
		"last_exit: 1",
		"apps/desktop/src-tauri/Cargo.toml",
		"q: what do i do",
	} {
		if !strings.Contains(gotPrompt, want) {
			t.Errorf("ForSuggest missing %q in prompt:\n%s", want, gotPrompt)
		}
	}

	// Verify concrete command-first replies parse cleanly for prompt fill.
	modelReply := "COMMAND: git stash push -- apps/desktop/src-tauri/Cargo.toml && git pull --ff-only && git stash pop\n" +
		"NOTE: Stashes Cargo.toml, pulls cleanly, and reapplies your edits (or run `git checkout -- apps/desktop/src-tauri/Cargo.toml` to discard them)."
	sug := ParseSuggestion(modelReply)
	wantCmd := "git stash push -- apps/desktop/src-tauri/Cargo.toml && git pull --ff-only && git stash pop"
	if sug.Command != wantCmd {
		t.Errorf("Command = %q want %q", sug.Command, wantCmd)
	}
	if !strings.Contains(sug.Note, "Cargo.toml") {
		t.Errorf("Note = %q expected Cargo.toml mention", sug.Note)
	}
	encoded := EncodeCapture(sug)
	if !strings.HasPrefix(encoded, "COMMAND:"+wantCmd+"\nNOTE:") {
		t.Errorf("EncodeCapture unexpected: %q", encoded)
	}
}

func TestParseSuggestion(t *testing.T) {
	cases := []struct {
		in       string
		wantCmd  string
		wantNote string
	}{
		{
			"COMMAND: git pull\nNOTE: Fetch and merge remote changes.",
			"git pull",
			"Fetch and merge remote changes.",
		},
		{
			"```\nCOMMAND: git status\nNOTE: Show working tree\n```",
			"git status",
			"Show working tree",
		},
		{
			"git merge origin/main",
			"git merge origin/main",
			"",
		},
		{
			"```bash\ngit rebase -i HEAD~3\n```",
			"git rebase -i HEAD~3",
			"",
		},
		{
			"$ ls -la\n\nlists files",
			"ls -la",
			"lists files",
		},
		{
			"CMD: echo hi\nNOTE: says hi",
			"echo hi",
			"says hi",
		},
	}
	for _, c := range cases {
		got := ParseSuggestion(c.in)
		if got.Command != c.wantCmd || got.Note != c.wantNote {
			t.Errorf("ParseSuggestion(%q)=(%q,%q) want (%q,%q)",
				c.in, got.Command, got.Note, c.wantCmd, c.wantNote)
		}
	}
}

func TestExtractCommand(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"git merge origin/main", "git merge origin/main"},
		{"```\ngit status\n```", "git status"},
		{"```bash\ngit rebase -i HEAD~3\n```", "git rebase -i HEAD~3"},
		{"$ ls -la\n\nlists files", "ls -la"},
		{"\n\n  echo hi  \n", "echo hi"},
		{"COMMAND: pwd\nNOTE: print cwd", "pwd"},
	}
	for _, c := range cases {
		if got := ExtractCommand(c.in); got != c.want {
			t.Errorf("ExtractCommand(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestEncodeCapture(t *testing.T) {
	got := EncodeCapture(Suggestion{Command: "git pull", Note: "sync"})
	want := "COMMAND:git pull\nNOTE:sync\n"
	if got != want {
		t.Errorf("EncodeCapture = %q want %q", got, want)
	}
}
