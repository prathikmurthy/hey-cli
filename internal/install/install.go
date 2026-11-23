package install

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/prathikmurthy/hey-cli/internal/config"
)

const (
	beginMarker = "# >>> hey-cli >>>"
	endMarker   = "# <<< hey-cli <<<"
)

// DetectedShells returns installed shells we support (zsh, bash), keyed by name → path.
func DetectedShells() map[string]string {
	found := map[string]string{}
	for _, name := range []string{"zsh", "bash"} {
		if p, err := exec.LookPath(name); err == nil {
			found[name] = p
		}
	}
	// Also check common absolute paths in case PATH is minimal.
	for _, name := range []string{"zsh", "bash"} {
		if _, ok := found[name]; ok {
			continue
		}
		for _, dir := range []string{"/bin", "/usr/bin", "/usr/local/bin", "/opt/homebrew/bin"} {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				found[name] = p
				break
			}
		}
	}
	return found
}

// DefaultShellName is the user's login/$SHELL basename when it is zsh or bash.
func DefaultShellName() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		out, err := exec.Command("sh", "-c", "getent passwd \"$(id -un)\" 2>/dev/null | cut -d: -f7").Output()
		if err == nil {
			shell = strings.TrimSpace(string(out))
		}
	}
	base := filepath.Base(shell)
	switch base {
	case "zsh", "bash":
		return base
	default:
		return ""
	}
}

// RCPath returns the rc file to modify for a shell.
func RCPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "zsh":
		if zdot := os.Getenv("ZDOTDIR"); zdot != "" {
			return filepath.Join(zdot, ".zshrc"), nil
		}
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		// Prefer .bashrc for interactive non-login; ensure .bash_profile sources it on macOS later if needed.
		return filepath.Join(home, ".bashrc"), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

// ShellSnippet returns the sourced block for a shell, pointing at shareDir.
func ShellSnippet(shell, shareDir string) string {
	switch shell {
	case "zsh":
		return fmt.Sprintf("%s\n[[ -f %q ]] && source %q\n%s\n",
			beginMarker,
			filepath.Join(shareDir, "hey.zsh"),
			filepath.Join(shareDir, "hey.zsh"),
			endMarker)
	case "bash":
		return fmt.Sprintf("%s\n[[ -f %q ]] && source %q\n%s\n",
			beginMarker,
			filepath.Join(shareDir, "hey.bash"),
			filepath.Join(shareDir, "hey.bash"),
			endMarker)
	default:
		return ""
	}
}

// ShareDir is where widget scripts are installed (~/.hey/shell).
func ShareDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shell"), nil
}

// PromptShells interactively asks which shells to wire (default = login shell).
// dryRun only prints what would happen.
func PromptShells(reader *bufio.Reader, dryRun bool) ([]string, error) {
	detected := DetectedShells()
	if len(detected) == 0 {
		return nil, fmt.Errorf("no supported shells found (need zsh and/or bash on PATH)")
	}

	def := DefaultShellName()
	names := []string{}
	for _, n := range []string{"zsh", "bash"} {
		if _, ok := detected[n]; ok {
			names = append(names, n)
		}
	}

	fmt.Println("Detected shells:")
	for _, n := range names {
		mark := ""
		if n == def {
			mark = " (default)"
		}
		fmt.Printf("  - %s%s  %s\n", n, mark, detected[n])
	}

	promptDefault := def
	if promptDefault == "" {
		promptDefault = names[0]
	}

	fmt.Printf("\nWhich shell(s) should hey wire up? [%s]\n", promptDefault)
	fmt.Println("  Enter a comma-separated list (e.g. zsh,bash), or press Enter for the default.")
	fmt.Print("> ")

	line, err := reader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(line)) == 0 {
		return nil, err
	}
	line = strings.TrimSpace(line)
	var chosen []string
	if line == "" {
		chosen = []string{promptDefault}
	} else {
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if part == "" {
				continue
			}
			if _, ok := detected[part]; !ok {
				return nil, fmt.Errorf("shell %q is not installed or not supported", part)
			}
			chosen = append(chosen, part)
		}
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("no shells selected")
	}

	if dryRun {
		fmt.Println("\n[dry-run] would install widgets and update rc files for:", strings.Join(chosen, ", "))
		share, _ := ShareDir()
		for _, sh := range chosen {
			rc, _ := RCPath(sh)
			fmt.Printf("  %s → source %s/hey.%s from %s\n", sh, share, sh, rc)
		}
		return chosen, nil
	}
	return chosen, nil
}

// InstallWidgets copies embedded widget files into ~/.hey/shell and patches rc files.
func InstallWidgets(shells []string, widgetFiles map[string][]byte, heyBin string) error {
	share, err := ShareDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(share, 0o755); err != nil {
		return err
	}

	for _, sh := range shells {
		name := "hey." + sh
		data, ok := widgetFiles[name]
		if !ok {
			return fmt.Errorf("missing embedded widget %s", name)
		}
		dest := filepath.Join(share, name)
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}

		rc, err := RCPath(sh)
		if err != nil {
			return err
		}
		snippet := ShellSnippet(sh, share)
		if err := upsertRC(rc, snippet); err != nil {
			return err
		}
		fmt.Printf("Installed %s integration → %s\n", sh, rc)
	}

	// Ensure hey is on PATH hint.
	if heyBin != "" {
		fmt.Printf("hey binary: %s\n", heyBin)
	} else if _, err := exec.LookPath("hey"); err != nil {
		fmt.Println("Note: ensure the hey binary is on your PATH.")
	}

	cfg, _ := config.Load()
	cfg.Shells = shells
	_ = config.Save(cfg)

	fmt.Println("\nReload your shell or open a new terminal, then try:")
	fmt.Println(`  hey how do I merge this`)
	if runtime.GOOS != "" {
		fmt.Println("  (zsh/bash widgets also bind Ctrl+G to fill the current line)")
	}
	return nil
}

func upsertRC(path, snippet string) error {
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return err
	}

	content := string(existing)
	if i := strings.Index(content, beginMarker); i >= 0 {
		j := strings.Index(content[i:], endMarker)
		if j >= 0 {
			end := i + j + len(endMarker)
			// consume trailing newline
			if end < len(content) && content[end] == '\n' {
				end++
			}
			content = content[:i] + snippet + content[end:]
		} else {
			content = content[:i] + snippet
		}
	} else {
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + snippet
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// DryRunReport prints detection without writing.
func DryRunReport() {
	detected := DetectedShells()
	def := DefaultShellName()
	fmt.Println("hey install --dry-run")
	fmt.Println("Detected shells:")
	if len(detected) == 0 {
		fmt.Println("  (none)")
	}
	for _, n := range []string{"zsh", "bash"} {
		if p, ok := detected[n]; ok {
			mark := ""
			if n == def {
				mark = " [default]"
			}
			fmt.Printf("  %s%s → %s\n", n, mark, p)
		}
	}
	share, _ := ShareDir()
	fmt.Printf("Widget share dir: %s\n", share)
	for _, n := range []string{"zsh", "bash"} {
		if _, ok := detected[n]; !ok {
			continue
		}
		rc, _ := RCPath(n)
		fmt.Printf("Would update %s and install hey.%s\n", rc, n)
	}
	if def != "" {
		fmt.Printf("Default selection: %s\n", def)
	}
}
