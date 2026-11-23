package agent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const installHint = "Cursor CLI (agent) not found. Install: curl https://cursor.com/install -fsS | bash  — docs: https://cursor.com/docs/cli/overview"

var ErrNotFound = errors.New("agent not found")

// Resolve finds the Cursor agent binary on PATH (agent or cursor-agent).
func Resolve() (string, error) {
	for _, name := range []string{"agent", "cursor-agent"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", ErrNotFound
}

// Ask runs `agent -p --mode ask --output-format text` with an optional model.
// Auth uses the existing `agent login` session or CURSOR_API_KEY.
func Ask(promptText, model string) (string, error) {
	bin, err := Resolve()
	if err != nil {
		return "", fmt.Errorf("%w\n%s", ErrNotFound, installHint)
	}

	args := []string{
		"-p",
		"--mode", "ask",
		"--output-format", "text",
		"--trust",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// Prompt as a single trailing argument.
	args = append(args, promptText)

	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Inherit env so CURSOR_API_KEY and login session are visible.
	cmd.Env = os.Environ()

	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if out != "" {
			return out, fmt.Errorf("agent failed: %s", msg)
		}
		return "", fmt.Errorf("agent failed: %s", msg)
	}
	return out, nil
}

// InstallHint is the one-line message when agent is missing.
func InstallHint() string {
	return installHint
}
