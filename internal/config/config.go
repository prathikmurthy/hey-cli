package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	AppName     = "hey"
	ConfigDir   = ".hey"
	ConfigFile  = "config.json"
	// DefaultHist is kept small for latency (less prompt tokens).
	DefaultHist = 8
)

// Config is the minimal on-disk settings for hey.
type Config struct {
	// Shells that have widget/integration installed (zsh, bash).
	Shells []string `json:"shells,omitempty"`
	// Model passed to `agent --model` when non-empty.
	Model string `json:"model,omitempty"`
	// HistoryLines is how many recent shell history lines to include as context.
	HistoryLines int `json:"history_lines,omitempty"`
}

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ConfigDir), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFile), nil
}

func Load() (Config, error) {
	cfg := Config{HistoryLines: DefaultHist}
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.HistoryLines <= 0 {
		cfg.HistoryLines = DefaultHist
	}
	return cfg, nil
}

func Save(cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, ConfigFile)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
