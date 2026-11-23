package style

import (
	"fmt"
	"os"
)

// Dim wraps text in ANSI faint/dim when stdout is a TTY and NO_COLOR is unset.
func Dim(s string) string {
	if s == "" || !useColor() {
		return s
	}
	return "\033[2m" + s + "\033[0m"
}

// PrintDim writes a dimmed line to stderr (notes should never compete with the command).
func PrintDim(s string) {
	if s == "" {
		return
	}
	fmt.Fprintln(os.Stderr, Dim(s))
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("HEY_FORCE_COLOR") != "" {
		return true
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
