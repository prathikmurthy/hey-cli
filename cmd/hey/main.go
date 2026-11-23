package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prathikmurthy/hey-cli/internal/agent"
	"github.com/prathikmurthy/hey-cli/internal/config"
	heyctx "github.com/prathikmurthy/hey-cli/internal/context"
	"github.com/prathikmurthy/hey-cli/internal/install"
	"github.com/prathikmurthy/hey-cli/internal/prompt"
	"github.com/prathikmurthy/hey-cli/shell"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return 0
	}

	switch args[0] {
	case "--help", "-h", "help":
		printHelp()
		return 0
	case "--version", "-V", "version":
		fmt.Println("hey", version)
		return 0
	case "install":
		return cmdInstall(args[1:])
	}

	fill := false
	explain := false
	printOnly := false
	var questionParts []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--fill":
			fill = true
		case "--explain":
			explain = true
		case "--print":
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
			cfg, _ := config.Load()
			cfg.Model = args[i]
			_ = config.Save(cfg)
			fmt.Println("model set to", cfg.Model)
			return 0
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
	if question == "" {
		fmt.Fprintln(os.Stderr, "hey: missing question — try: hey how do I merge this")
		return 2
	}

	// Default interactive path from shell wrappers uses --fill.
	// Bare invocation without --fill uses explain/print output.
	if !fill && !explain {
		explain = true
	}
	if printOnly {
		explain = true
		fill = false
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hey: config: %v\n", err)
		return 1
	}

	bundle := heyctx.Collect(question, cfg.HistoryLines)
	ctxBlock := bundle.Format()

	var promptText string
	if fill {
		promptText = prompt.ForFill(ctxBlock)
	} else {
		promptText = prompt.ForExplain(ctxBlock)
	}

	out, err := agent.Ask(promptText, cfg.Model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hey:", err)
		return 1
	}

	if fill {
		cmd := prompt.ExtractCommand(out)
		if cmd == "" {
			fmt.Fprintln(os.Stderr, "hey: model returned no command")
			return 1
		}
		fmt.Println(cmd)
		return 0
	}

	fmt.Println(out)
	return 0
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
  hey <question...>           Ask and print command + brief explanation
  hey --fill <question...>    Print only the command (used by shell widgets)
  hey --print <question...>   Force print/explain mode
  hey install [--dry-run]     Detect shells and wire up zsh/bash integration
  hey --model <name>          Save default model for agent --model
  hey --version

Examples:
  hey how do I merge this
  hey --fill how do I undo the last commit

Auth (Cursor subscription — no OpenAI/Anthropic keys):
  agent login
  # or: export CURSOR_API_KEY=...   (Cursor Dashboard → API Keys)

Requires the Cursor CLI on PATH:
  curl https://cursor.com/install -fsS | bash
  https://cursor.com/docs/cli/overview

Shell integration (after hey install):
  zsh  — hey() uses print -z to fill the next prompt; Ctrl+G fills BUFFER
  bash — hey() stages via history; Ctrl+G fills READLINE_LINE

Context collection:
  Widgets set HEY_HISTORY (recent fc/history lines) and HEY_BUFFER.
  Outside widgets, hey reads $HISTFILE / ~/.zsh_history / ~/.bash_history.
`)
}
