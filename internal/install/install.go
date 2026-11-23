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
		// Prefer .bashrc for interactive non-login; ensureBashProfileSourcesRC covers macOS login shells.
		return filepath.Join(home, ".bashrc"), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

// BinDirGuess returns the directory containing heyBin, or ~/.local/bin.
func BinDirGuess(heyBin string) string {
	if heyBin != "" {
		return filepath.Dir(heyBin)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}

// ShellSnippet returns the sourced block for a shell: PATH + widget.
func ShellSnippet(shell, shareDir, binDir string) string {
	widget := filepath.Join(shareDir, "hey."+shell)
	var b strings.Builder
	b.WriteString(beginMarker)
	b.WriteByte('\n')
	if binDir != "" {
		b.WriteString(fmt.Sprintf(`export PATH=%q:$PATH`, binDir))
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("[[ -f %q ]] && source %q\n", widget, widget))
	b.WriteString(endMarker)
	b.WriteByte('\n')
	return b.String()
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

	binDir := BinDirGuess(heyBin)

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
		snippet := ShellSnippet(sh, share, binDir)
		if err := upsertRC(rc, snippet); err != nil {
			return err
		}
		if sh == "bash" {
			if err := ensureBashProfileSourcesRC(); err != nil {
				return err
			}
		}
		fmt.Printf("Installed %s integration → %s\n", sh, rc)
	}

	if heyBin != "" {
		fmt.Printf("hey binary: %s\n", heyBin)
	} else if _, err := exec.LookPath("hey"); err != nil {
		fmt.Println("Note: ensure the hey binary is on your PATH.")
	}

	cfg, _ := config.Load()
	cfg.Shells = shells
	_ = config.Save(cfg)

	fmt.Println("\nDone. Open a new terminal (or run: exec $SHELL -l), then try:")
	fmt.Println(`  hey how do I merge this`)
	if runtime.GOOS != "" {
		fmt.Println("  (Ctrl+G fills the current line from a typed question)")
	}
	return nil
}

func ensureBashProfileSourcesRC() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	bp := filepath.Join(home, ".bash_profile")
	data, err := os.ReadFile(bp)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	if strings.Contains(content, ".bashrc") {
		return nil
	}
	block := "\n# >>> hey-cli path >>>\n[[ -f ~/.bashrc ]] && source ~/.bashrc\n# <<< hey-cli path <<<\n"
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(bp, []byte(content+block), 0o644)
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
