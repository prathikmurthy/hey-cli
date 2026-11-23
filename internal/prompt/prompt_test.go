package prompt

import "testing"

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
