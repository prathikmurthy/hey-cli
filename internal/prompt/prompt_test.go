package prompt

import "testing"

func TestExtractCommand(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"git merge origin/main", "git merge origin/main"},
		{"```\ngit status\n```", "git status"},
		{"```bash\ngit rebase -i HEAD~3\n```", "git rebase -i HEAD~3"},
		{"$ ls -la\n\nlists files", "ls -la"},
		{"\n\n  echo hi  \n", "echo hi"},
	}
	for _, c := range cases {
		if got := ExtractCommand(c.in); got != c.want {
			t.Errorf("ExtractCommand(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
