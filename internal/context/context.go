package context

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prathikmurthy/hey-cli/internal/capture"
)

// Bundle is terminal context passed to the model.
type Bundle struct {
	Question     string
	History      []string
	Buffer       string // optional current prompt buffer from the widget
	CWD          string
	Shell        string
	LastCommand  string // optional last non-hey command run in this shell
	LastExitCode *int   // optional exit status of LastCommand
	LastOutput   string // optional sanitized tail of last command / terminal output
}

// Collect gathers recent shell history, last command output/scrollback, and optional widget buffer.
//
// History sources (in order of preference):
//  1. HEY_HISTORY env (widget-supplied, newline-separated recent lines)
//  2. HISTFILE / shell-specific history files (~/.zsh_history, ~/.bash_history)
//
// Last-output sources (in order of preference):
//  1. HEY_LAST_OUTPUT env (explicit override, e.g. scripts/tests)
//  2. HEY_LAST_OUTPUT_FILE env (per-shell captured output from preexec/precmd hook)
//  3. Live terminal scrollback (tmux, WezTerm, Kitty) when available
//  4. Recent ~/.hey/last_output fallback (only if modified within 10 minutes)
//
// All captured text has ANSI escape sequences stripped and secrets/env dumps redacted.
func Collect(question string, maxLines int, maxOutputLines ...int) Bundle {
	cwd, _ := os.Getwd()
	b := Bundle{
		Question: question,
		Buffer:   strings.TrimSpace(os.Getenv("HEY_BUFFER")),
		CWD:      cwd,
		Shell:    ShellName(),
	}
	if maxLines <= 0 {
		maxLines = 8
	}
	outLines := capture.DefaultMaxLines
	if len(maxOutputLines) > 0 && maxOutputLines[0] > 0 {
		outLines = maxOutputLines[0]
	}

	if raw := os.Getenv("HEY_HISTORY"); raw != "" {
		b.History = sanitizeHistory(lastNonEmpty(strings.Split(raw, "\n"), maxLines))
	} else {
		b.History = sanitizeHistory(readHistoryFile(detectHistFile(), maxLines))
	}

	if lastCmd := strings.TrimSpace(os.Getenv("HEY_LAST_CMD")); lastCmd != "" {
		b.LastCommand = capture.RedactLine(lastCmd)
	}
	if rawExit := strings.TrimSpace(os.Getenv("HEY_LAST_EXIT")); rawExit != "" {
		if ec, err := strconv.Atoi(rawExit); err == nil {
			b.LastExitCode = &ec
		}
	}

	b.LastOutput = collectLastOutput(outLines)
	return b
}

func collectLastOutput(maxLines int) string {
	// 1. Explicit override (also used by fixtures/tests).
	if raw, ok := os.LookupEnv("HEY_LAST_OUTPUT"); ok {
		return capture.Sanitize(raw, maxLines, capture.DefaultMaxBytes)
	}

	// 2. Per-shell captured output file from preexec/precmd hook.
	if filePath := strings.TrimSpace(os.Getenv("HEY_LAST_OUTPUT_FILE")); filePath != "" {
		if raw := capture.ReadRecentFile(filePath, 0); raw != "" {
			if s := capture.Sanitize(raw, maxLines, capture.DefaultMaxBytes); s != "" {
				return s
			}
		}
	}

	// 3. Terminal multiplexer / emulator scrollback (tmux, WezTerm, Kitty).
	if raw := capture.ReadScrollback(maxLines); raw != "" {
		if s := capture.Sanitize(raw, maxLines, capture.DefaultMaxBytes); s != "" {
			return s
		}
	}

	// 4. Recent fallback ~/.hey/last_output (bare binary outside wrapper/tmux).
	if fallback, err := capture.FallbackOutputPath(); err == nil {
		if raw := capture.ReadRecentFile(fallback, capture.DefaultFallbackMaxAge); raw != "" {
			return capture.Sanitize(raw, maxLines, capture.DefaultMaxBytes)
		}
	}

	return ""
}

func sanitizeHistory(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, capture.RedactLine(l))
	}
	return out
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
	sb.WriteString("cwd: ")
	sb.WriteString(b.CWD)
	sb.WriteByte('\n')
	if b.Shell != "" {
		sb.WriteString("shell: ")
		sb.WriteString(b.Shell)
		sb.WriteByte('\n')
	}
	if len(b.History) > 0 {
		sb.WriteString("history:\n")
		for _, h := range b.History {
			sb.WriteString("- ")
			sb.WriteString(h)
			sb.WriteByte('\n')
		}
	}
	if b.LastCommand != "" {
		sb.WriteString("last_command: ")
		sb.WriteString(b.LastCommand)
		sb.WriteByte('\n')
	}
	if b.LastExitCode != nil {
		sb.WriteString("last_exit: ")
		sb.WriteString(strconv.Itoa(*b.LastExitCode))
		sb.WriteByte('\n')
	}
	if b.LastOutput != "" {
		sb.WriteString("last_output:\n")
		sb.WriteString(b.LastOutput)
		sb.WriteByte('\n')
	}
	if b.Buffer != "" {
		sb.WriteString("buffer: ")
		sb.WriteString(b.Buffer)
		sb.WriteByte('\n')
	}
	sb.WriteString("q: ")
	sb.WriteString(b.Question)
	sb.WriteByte('\n')
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
