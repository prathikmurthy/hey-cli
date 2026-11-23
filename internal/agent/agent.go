package agent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const installHint = "Cursor CLI (agent) not found. Install: curl https://cursor.com/install -fsS | bash  — docs: https://cursor.com/docs/cli/overview"

// DefaultModel is the fastest commonly available Cursor model for short asks.
const DefaultModel = "composer-2-fast"

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

// Ask runs a minimal Cursor ask for a short text reply.
// Auth uses the existing `agent login` session or CURSOR_API_KEY.
//
// Speed knobs: composer-2-fast by default, --mode ask, --sandbox disabled,
// and a tiny empty --workspace so the CLI does not index the caller's repo.
func Ask(promptText, model string) (string, error) {
	bin, err := Resolve()
	if err != nil {
		return "", fmt.Errorf("%w\n%s", ErrNotFound, installHint)
	}
	if model == "" {
		model = DefaultModel
	}

	workspace, err := emptyWorkspace()
	if err != nil {
		return "", err
	}

	args := []string{
		"-p",
		"--mode", "ask",
		"--output-format", "text",
		"--trust",
		"--sandbox", "disabled",
		"--workspace", workspace,
		"--model", model,
		promptText,
	}

	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if os.Getenv("HEY_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "hey: agent round-trip %s (model=%s)\n", elapsed.Round(time.Millisecond), model)
	}

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

func emptyWorkspace() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".hey", "workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Tiny placeholder so the workspace is a valid non-repo directory.
	marker := filepath.Join(dir, ".hey-workspace")
	if _, err := os.Stat(marker); err != nil {
		_ = os.WriteFile(marker, []byte("hey empty workspace\n"), 0o644)
	}
	return dir, nil
}

// InstallHint is the one-line message when agent is missing.
func InstallHint() string {
	return installHint
}
