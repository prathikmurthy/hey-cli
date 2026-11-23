package pending

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prathikmurthy/hey-cli/internal/config"
	"github.com/prathikmurthy/hey-cli/internal/prompt"
)

const fileName = "pending"

// Path is ~/.hey/pending — shell precmd hooks read this to fill the next prompt.
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Write stores a suggestion for the next interactive prompt fill.
func Write(sug prompt.Suggestion) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("COMMAND:")
	sb.WriteString(sug.Command)
	sb.WriteByte('\n')
	if sug.Note != "" {
		sb.WriteString("NOTE:")
		sb.WriteString(sug.Note)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

// Clear removes any pending suggestion.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// OSC emits a machine-readable OSC sequence shell hooks may intercept
// (ignored by terminals that do not understand it).
func OSC(sug prompt.Suggestion) string {
	// OSC 1337 private: Hey;cmd=<...>;note=<...> BEL
	cmd := strings.ReplaceAll(sug.Command, "\n", " ")
	note := strings.ReplaceAll(sug.Note, "\n", " ")
	return fmt.Sprintf("\033]1337;Hey=1;cmd=%s;note=%s\a", cmd, note)
}
