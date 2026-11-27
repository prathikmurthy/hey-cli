package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/prathikmurthy/hey-cli/internal/agent"
	heycap "github.com/prathikmurthy/hey-cli/internal/capture"
	"github.com/prathikmurthy/hey-cli/internal/config"
	heyctx "github.com/prathikmurthy/hey-cli/internal/context"
	"github.com/prathikmurthy/hey-cli/internal/install"
	"github.com/prathikmurthy/hey-cli/internal/pending"
	"github.com/prathikmurthy/hey-cli/internal/prompt"
	"github.com/prathikmurthy/hey-cli/internal/style"
	"github.com/prathikmurthy/hey-cli/shell"
)

// version is overridden at release build time via -ldflags "-X main.version=…".
var version = "0.1.1"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return 0
	}

	switch args[0] {
	case "_tee":
		return cmdTee(args[1:])
	case "--help", "-h", "help":
		printHelp()
		return 0
	case "--version", "-V", "version":
		fmt.Println("hey", version)
		return 0
	case "install":
		return cmdInstall(args[1:])
	}

	capture := false // machine-readable for shell widgets
	printOnly := false
	var questionParts []string
	modelFlag := ""
	persistModel := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--fill", "--capture":
			capture = true
		case "--explain", "--print":
			printOnly = true
		case "--":
			questionParts = append(questionParts, args[i+1:]...)
			i = len(args)
		case "--model":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "hey: --model requires a value")
				return 2
			}
			i++
			modelFlag = args[i]
			persistModel = true // persist unless a question follows
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "hey: unknown flag %s\n", a)
				return 2
			}
			questionParts = append(questionParts, args[i:]...)
			i = len(args)
		}
	}

	question := strings.TrimSpace(strings.Join(questionParts, " "))
	if question != "" {
		persistModel = false // one-shot override when asking
	}
	if persistModel && modelFlag != "" {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "hey: config: %v\n", err)
			return 1
		}
		cfg.Model = modelFlag
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "hey: config: %v\n", err)
			return 1
		}
		fmt.Println("model set to", cfg.Model)
		return 0
	}
	if question == "" {
		fmt.Fprintln(os.Stderr, "hey: missing question — try: hey how do I merge this")
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hey: config: %v\n", err)
		return 1
	}

	model := modelFlag
	if model == "" {
		model = cfg.Model
	}

	if runDir, err := heycap.RunDir(); err == nil {
		heycap.PruneStale(runDir, heycap.DefaultPruneMaxAge)
	}

	bundle := heyctx.Collect(question, cfg.HistoryLines, cfg.OutputLines)
	ctxBlock := bundle.Format()
	promptText := prompt.ForSuggest(ctxBlock)

	out, err := agent.Ask(promptText, model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hey:", err)
		return 1
	}

	sug := prompt.ParseSuggestion(out)
	if sug.Command == "" {
		fmt.Fprintln(os.Stderr, "hey: model returned no command")
		return 1
	}

	integration := os.Getenv("HEY_INTEGRATION") != ""
	if os.Getenv("HEY_CAPTURE") != "" {
		capture = true
	}

	switch {
	case capture:
		// Shell widget / wrapper path: machine-readable stdout only.
		fmt.Print(prompt.EncodeCapture(sug))
		_ = pending.Clear()
		return 0
	case printOnly:
		fmt.Println(sug.Command)
		if sug.Note != "" {
			style.PrintDim(sug.Note)
		}
		_ = pending.Clear()
		return 0
	default:
		// Bare argv: stage for shell precmd/PROMPT_COMMAND hooks, emit OSC,
		// and print a human fallback (command + dim note).
		_ = pending.Write(sug)
		fmt.Fprint(os.Stderr, pending.OSC(sug))
		fmt.Println(sug.Command)
		if sug.Note != "" {
			style.PrintDim(sug.Note)
		}
		if !integration {
			style.PrintDim("tip: run  curl -fsSL https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash")
			style.PrintDim("     then open a new terminal (or: exec $SHELL -l) so the prompt fills automatically")
		} else {
			style.PrintDim("(filling next prompt…)")
		}
		return 0
	}
}

func cmdInstall(args []string) int {
	dryRun := false
	nonInteractive := false
	var shellsFlag string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--yes", "-y":
			nonInteractive = true
		case "--shells":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "hey: --shells requires a value (e.g. zsh,bash)")
				return 2
			}
			i++
			shellsFlag = args[i]
		case "--help", "-h":
			fmt.Print(`Usage: hey install [--dry-run] [--yes] [--shells zsh,bash]

Detect installed shells (zsh/bash), default to $SHELL, and wire up
prompt-fill widgets (Ctrl+G) plus an interactive hey() wrapper.
`)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "hey install: unknown flag %s\n", args[i])
			return 2
		}
	}

	if dryRun && shellsFlag == "" && !nonInteractive {
		install.DryRunReport()
		return 0
	}

	widgets, err := loadWidgets()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hey install:", err)
		return 1
	}

	var chosen []string
	if shellsFlag != "" {
		detected := install.DetectedShells()
		for _, p := range strings.Split(shellsFlag, ",") {
			p = strings.TrimSpace(strings.ToLower(p))
			if p == "" {
				continue
			}
			if _, ok := detected[p]; !ok {
				fmt.Fprintf(os.Stderr, "hey install: shell %q not found\n", p)
				return 1
			}
			chosen = append(chosen, p)
		}
	} else if nonInteractive {
		def := install.DefaultShellName()
		if def == "" {
			detected := install.DetectedShells()
			for _, n := range []string{"zsh", "bash"} {
				if _, ok := detected[n]; ok {
					def = n
					break
				}
			}
		}
		if def == "" {
			fmt.Fprintln(os.Stderr, "hey install: no supported shell found")
			return 1
		}
		chosen = []string{def}
	} else {
		chosen, err = install.PromptShells(bufio.NewReader(os.Stdin), dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hey install:", err)
			return 1
		}
		if dryRun {
			return 0
		}
	}

	if dryRun {
		fmt.Println("[dry-run] selected shells:", strings.Join(chosen, ", "))
		share, _ := install.ShareDir()
		for _, sh := range chosen {
			rc, _ := install.RCPath(sh)
			fmt.Printf("  would install hey.%s → %s and source from %s\n", sh, share, rc)
		}
		return 0
	}

	heyBin, _ := os.Executable()
	if heyBin != "" {
		if resolved, err := filepath.EvalSymlinks(heyBin); err == nil {
			heyBin = resolved
		}
	}

	if err := install.InstallWidgets(chosen, widgets, heyBin); err != nil {
		fmt.Fprintln(os.Stderr, "hey install:", err)
		return 1
	}
	return 0
}

func cmdTee(args []string) int {
	var outPath, fallbackPath, donePath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 < len(args) {
				i++
				outPath = args[i]
			}
		case "--fallback":
			if i+1 < len(args) {
				i++
				fallbackPath = args[i]
			}
		case "--done":
			if i+1 < len(args) {
				i++
				donePath = args[i]
			}
		}
	}
	// Never die on Ctrl-C / Ctrl-Z so foreground commands can still flush output on interrupt.
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	_ = heycap.Tee(os.Stdin, os.Stdout, outPath, fallbackPath, donePath, heycap.DefaultTeeBufferBytes)
	return 0
}

func loadWidgets() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range []string{"hey.zsh", "hey.bash"} {
		data, err := shell.Files.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		out[name] = data
	}
	return out, nil
}

func printHelp() {
	fmt.Print(`hey — suggest a shell command from a plain-text question (Cursor-backed)

Usage:
  hey <question...>           Fill the next prompt with a command (+ dim note)
  hey --fill <question...>    Machine-readable COMMAND/NOTE (shell widgets)
  hey --print <question...>   Print command + dim note (no prompt fill)
  hey install [--dry-run]     Detect shells and wire up zsh/bash integration
  hey --model <name>          Save default model for agent --model
  hey --version

Examples:
  hey how do I merge this
  hey --fill how do I undo the last commit
  hey --model composer-2-fast how do I get new git changes

Auth (Cursor subscription — no OpenAI/Anthropic keys):
  agent login
  # or: export CURSOR_API_KEY=...   (Cursor Dashboard → API Keys)

Requires the Cursor CLI on PATH:
  curl https://cursor.com/install -fsS | bash
  https://cursor.com/docs/cli/overview

Install (no Go required):
  curl -fsSL https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash
  exec $SHELL -l    # or open a new terminal

Context:
  Shell hooks capture the last non-interactive command's exit code and tail (~40 lines)
  to ~/.hey/run/ (or read tmux/WezTerm/Kitty scrollback), stripping ANSI codes and
  redacting secrets. Set HEY_CAPTURE_OUTPUT=0 to disable command output capture.
  Default model: composer-2-fast (override with hey --model or ~/.hey/config.json).
`)
}
