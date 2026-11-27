package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/prathikmurthy/hey-cli/internal/config"
)

const (
	// DefaultMaxLines is how many recent terminal/command output lines are sent to the model.
	DefaultMaxLines = 40
	// DefaultMaxBytes caps output text included in the prompt so fast models stay fast.
	DefaultMaxBytes = 4096
	// DefaultTeeBufferBytes caps the per-shell output file size on disk.
	DefaultTeeBufferBytes = 16384
	// DefaultFallbackMaxAge ignores stale fallback output files from older sessions.
	DefaultFallbackMaxAge = 10 * time.Minute
	// DefaultPruneMaxAge removes abandoned per-shell capture files older than this.
	DefaultPruneMaxAge = 24 * time.Hour

	scrollbackTimeout = 250 * time.Millisecond
	idleFlushInterval = 150 * time.Millisecond
)

var (
	// Terminal escape sequences: OSC, DCS/SOS/PM/APC, CSI, charset designations, 2-byte ESC.
	ansiOSC     = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	ansiDCS     = regexp.MustCompile(`\x1b[PX^_][^\x1b]*\x1b\\`)
	ansiCSI     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiCharset = regexp.MustCompile(`\x1b[\(\)\*\+][0-9A-Za-z]`)
	ansiTwoByte = regexp.MustCompile(`\x1b[@-Z\\-_]`)

	// PEM private key blocks (complete or truncated at tail).
	pemBlockRe = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`)

	// Env-style assignment line (UPPER_SNAKE=value) for detecting raw env dumps.
	envAssignRe = regexp.MustCompile(`^(?:export\s+)?[A-Z_][A-Z0-9_]{1,}=`)

	// Sensitive KEY=VALUE or Header: VALUE patterns.
	secretAssignRe = regexp.MustCompile(
		`(?i)^(\s*(?:export\s+)?[A-Za-z0-9_.-]*(?:SECRET|TOKEN|PASS(?:WORD|WD)?|API_?KEY|ACCESS_?KEY|PRIVATE_?KEY|AUTH(?:ORIZATION)?|CREDENTIAL|COOKIE|SESSION_?(?:ID|KEY|TOKEN)?|DATABASE_?URL|DB_?PASS|WEBHOOK_?(?:SECRET|URL)?|SIGNING_?KEY|ENCRYPTION_?KEY|MASTER_?KEY)[A-Za-z0-9_.-]*\s*[:=]\s*)(.+)$`,
	)

	// Inline secret/token patterns.
	inlineSecretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bsk-(?:proj-|ant-|live-|test-)?[A-Za-z0-9_-]{16,}\b`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
		regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
		regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	}
	bearerRe  = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/-]{12,}=*`)
	urlCredRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^:@/\s]+):([^@/\s]+)@`)

	// Trailing prompt line invoking hey in terminal scrollback (e.g. "~/repo % hey what do i do").
	heyPromptLineRe = regexp.MustCompile(`^(?:.*[$%#❯➜›>]\s+)?hey(?:\s|$)`)
)

// RunDir returns ~/.hey/run where per-shell output capture files live.
func RunDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "run"), nil
}

// FallbackOutputPath returns ~/.hey/last_output used when bare hey runs without wrapper env.
func FallbackOutputPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "last_output"), nil
}

// Tee streams r to w immediately while retaining the last maxBytes in memory.
// On idle and on EOF, it atomically writes the tail to outPath (and optional fallbackPath)
// with 0600 permissions, then creates donePath so precmd knows flushing finished.
// Disk write failures never interrupt streaming to w.
func Tee(r io.Reader, w io.Writer, outPath, fallbackPath, donePath string, maxBytes int) error {
	if maxBytes <= 0 {
		maxBytes = DefaultTeeBufferBytes
	}

	var (
		mu         sync.Mutex
		ring       []byte
		dirty      bool
		lastWrite  time.Time
		flushDoneC = make(chan struct{})
	)

	snapshot := func() ([]byte, bool) {
		mu.Lock()
		defer mu.Unlock()
		if !dirty {
			return nil, false
		}
		cp := make([]byte, len(ring))
		copy(cp, ring)
		dirty = false
		return cp, true
	}

	writeTargets := func(data []byte) {
		if outPath != "" {
			_ = atomicWritePrivate(outPath, data)
		}
		if fallbackPath != "" && fallbackPath != outPath {
			_ = atomicWritePrivate(fallbackPath, data)
		}
	}

	// Background idle flusher so output is still captured if a background child keeps the pipe open.
	stopFlush := make(chan struct{})
	go func() {
		defer close(flushDoneC)
		ticker := time.NewTicker(idleFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopFlush:
				return
			case <-ticker.C:
				mu.Lock()
				shouldFlush := dirty && time.Since(lastWrite) >= idleFlushInterval
				mu.Unlock()
				if shouldFlush {
					if data, ok := snapshot(); ok {
						writeTargets(data)
					}
				}
			}
		}
	}()

	chunk := make([]byte, 4096)
	for {
		n, readErr := r.Read(chunk)
		if n > 0 {
			if w != nil {
				_, _ = w.Write(chunk[:n])
			}
			mu.Lock()
			ring = appendTail(ring, chunk[:n], maxBytes)
			dirty = true
			lastWrite = time.Now()
			mu.Unlock()
		}
		if readErr != nil {
			break
		}
	}

	close(stopFlush)
	<-flushDoneC

	mu.Lock()
	final := make([]byte, len(ring))
	copy(final, ring)
	mu.Unlock()

	writeTargets(final)
	if donePath != "" {
		_ = atomicWritePrivate(donePath, []byte("1\n"))
	}
	return nil
}

func appendTail(buf, add []byte, maxBytes int) []byte {
	if len(add) >= maxBytes {
		return append(buf[:0], add[len(add)-maxBytes:]...)
	}
	buf = append(buf, add...)
	if len(buf) > maxBytes {
		excess := len(buf) - maxBytes
		copy(buf, buf[excess:])
		buf = buf[:maxBytes]
	}
	return buf
}

func atomicWritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".hey-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// StripANSI removes terminal escape sequences, resolves carriage-return overwrites,
// and drops non-printable control characters while preserving tabs and newlines.
func StripANSI(s string) string {
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiDCS.ReplaceAllString(s, "")
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiCharset.ReplaceAllString(s, "")
	s = ansiTwoByte.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.ContainsRune(line, '\r') {
			parts := strings.Split(line, "\r")
			chosen := ""
			for j := len(parts) - 1; j >= 0; j-- {
				if parts[j] != "" {
					chosen = parts[j]
					break
				}
			}
			line = chosen
		}
		lines[i] = stripControlChars(line)
	}
	return strings.Join(lines, "\n")
}

func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RedactLine redacts secret values and credentials from a single command or output line.
func RedactLine(line string) string {
	if line == "" {
		return ""
	}
	if secretAssignRe.MatchString(line) {
		line = secretAssignRe.ReplaceAllString(line, "${1}[REDACTED]")
	}
	line = bearerRe.ReplaceAllString(line, "${1}[REDACTED]")
	line = urlCredRe.ReplaceAllString(line, "${1}[REDACTED]@")
	for _, re := range inlineSecretPatterns {
		line = re.ReplaceAllString(line, "[REDACTED]")
	}
	return line
}

// Sanitize strips ANSI sequences, redacts secrets/env dumps, and trims to the last
// maxLines lines and maxBytes bytes.
func Sanitize(raw string, maxLines, maxBytes int) string {
	if raw == "" {
		return ""
	}
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	cleaned := StripANSI(raw)
	cleaned = pemBlockRe.ReplaceAllString(cleaned, "[REDACTED PRIVATE KEY]")

	rawLines := strings.Split(cleaned, "\n")
	lines := make([]string, 0, len(rawLines))
	envCount := 0
	nonEmpty := 0
	for _, l := range rawLines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) != "" {
			nonEmpty++
			if envAssignRe.MatchString(strings.TrimSpace(l)) {
				envCount++
			}
		}
		lines = append(lines, l)
	}

	// Drop raw env / printenv / .env dumps entirely so secrets are never sent.
	if envCount >= 6 && nonEmpty > 0 && envCount*10 >= nonEmpty*6 {
		return ""
	}

	// Trim leading and trailing empty lines.
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	lines = lines[start:end]
	if len(lines) == 0 {
		return ""
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	for i, l := range lines {
		lines[i] = RedactLine(l)
	}

	out := strings.Join(lines, "\n")
	if len(out) > maxBytes {
		out = tailBytes(out, maxBytes)
	}
	return strings.TrimSpace(out)
}

func tailBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := len(s) - maxBytes
	// Advance to next UTF-8 rune boundary.
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	tail := s[cut:]
	// Prefer starting on a clean line boundary if there is one.
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 && nl+1 < len(tail) {
		tail = tail[nl+1:]
	}
	return tail
}

// ReadScrollback attempts a fast, bounded-timeout capture from the active terminal
// multiplexer/emulator (tmux, WezTerm, Kitty) when available. Returns "" if unavailable.
func ReadScrollback(maxLines int) string {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	grab := maxLines + 20

	var raw string
	switch {
	case os.Getenv("TMUX") != "":
		raw = runQuick("tmux", "capture-pane", "-p", "-J", "-S", fmt.Sprintf("-%d", grab))
	case os.Getenv("WEZTERM_PANE") != "":
		raw = runQuick("wezterm", "cli", "get-text", "--pane-id", os.Getenv("WEZTERM_PANE"), "--start-line", strconv.Itoa(-grab))
	case os.Getenv("KITTY_LISTEN_ON") != "":
		raw = runQuick("kitty", "@", "get-text", "--extent=screen")
	default:
		return ""
	}
	if raw == "" {
		return ""
	}
	return TrimTrailingHeyPrompt(raw)
}

func runQuick(name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), scrollbackTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

// TrimTrailingHeyPrompt removes trailing blank lines and the trailing `hey ...`
// command line from terminal scrollback so the preceding command's output stays at the bottom.
func TrimTrailingHeyPrompt(raw string) string {
	cleaned := StripANSI(raw)
	lines := strings.Split(cleaned, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	// Drop up to 3 trailing lines that are the current `hey` invocation or bare prompt.
	for i := 0; i < 3 && len(lines) > 0; i++ {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" || heyPromptLineRe.MatchString(last) {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}
	return strings.Join(lines, "\n")
}

// ReadRecentFile reads path if it exists and (when maxAge > 0) was modified within maxAge.
func ReadRecentFile(path string, maxAge time.Duration) string {
	if path == "" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return ""
	}
	if maxAge > 0 && time.Since(fi.ModTime()) > maxAge {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// PruneStale removes old capture files from dir (best-effort, ignores errors).
func PruneStale(dir string, maxAge time.Duration) {
	if dir == "" || maxAge <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
