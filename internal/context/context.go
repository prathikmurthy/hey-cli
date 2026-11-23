package context

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Bundle is terminal context passed to the model.
type Bundle struct {
	Question string
	History  []string
	Buffer   string // optional current prompt buffer from the widget
	CWD      string
	Shell    string
}

// Collect gathers recent shell history and optional widget buffer.
//
// Sources (in order of preference for history):
//  1. HEY_HISTORY env (widget-supplied, newline-separated recent lines)
//  2. HISTFILE / shell-specific history files (~/.zsh_history, ~/.bash_history)
//  3. `history` builtin output is not available from a child process; files only
//
// Optional HEY_BUFFER is the current editing buffer from ZLE/readline.
func Collect(question string, maxLines int) Bundle {
	cwd, _ := os.Getwd()
	b := Bundle{
		Question: question,
		Buffer:   strings.TrimSpace(os.Getenv("HEY_BUFFER")),
		CWD:      cwd,
		Shell:    filepath.Base(os.Getenv("SHELL")),
	}
	if maxLines <= 0 {
		maxLines = 30
	}
	if raw := os.Getenv("HEY_HISTORY"); raw != "" {
		b.History = lastNonEmpty(strings.Split(raw, "\n"), maxLines)
		return b
	}
	b.History = readHistoryFile(detectHistFile(), maxLines)
	return b
}

func detectHistFile() string {
	if f := os.Getenv("HISTFILE"); f != "" {
		return f
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zsh_history")
	case "bash":
		return filepath.Join(home, ".bash_history")
	default:
		// Prefer whichever exists.
		for _, name := range []string{".zsh_history", ".bash_history"} {
			p := filepath.Join(home, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func readHistoryFile(path string, maxLines int) []string {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	// Large history lines are rare; bump token size for safety.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// zsh extended history: : 1234567890:0;command
		if strings.HasPrefix(line, ": ") {
			if i := strings.Index(line, ";"); i >= 0 && i+1 < len(line) {
				line = line[i+1:]
			}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lastNonEmpty(lines, maxLines)
}

func lastNonEmpty(lines []string, n int) []string {
	cleaned := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			cleaned = append(cleaned, l)
		}
	}
	if len(cleaned) <= n {
		return cleaned
	}
	return cleaned[len(cleaned)-n:]
}

// Format returns a compact text block for the model prompt.
func (b Bundle) Format() string {
	var sb strings.Builder
	sb.WriteString("Working directory: ")
	sb.WriteString(b.CWD)
	sb.WriteString("\n")
	if b.Shell != "" {
		sb.WriteString("Shell: ")
		sb.WriteString(b.Shell)
		sb.WriteString("\n")
	}
	if len(b.History) > 0 {
		sb.WriteString("\nRecent terminal history (oldest → newest):\n")
		for _, h := range b.History {
			sb.WriteString("  $ ")
			sb.WriteString(h)
			sb.WriteString("\n")
		}
	}
	if b.Buffer != "" {
		sb.WriteString("\nCurrent prompt buffer:\n  ")
		sb.WriteString(b.Buffer)
		sb.WriteString("\n")
	}
	sb.WriteString("\nUser question:\n  ")
	sb.WriteString(b.Question)
	sb.WriteString("\n")
	return sb.String()
}

// ShellName returns a best-effort interactive shell name.
func ShellName() string {
	if s := os.Getenv("HEY_SHELL"); s != "" {
		return filepath.Base(s)
	}
	if s := os.Getenv("SHELL"); s != "" {
		return filepath.Base(s)
	}
	out, err := exec.Command("sh", "-c", "getent passwd \"$(id -un)\" | cut -d: -f7").Output()
	if err == nil {
		return filepath.Base(strings.TrimSpace(string(out)))
	}
	return ""
}
