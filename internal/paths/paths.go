// Package paths resolves the ocws home directory, its config file, and the
// templates root.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/c0dn/ocws/internal/engine"
	"github.com/pelletier/go-toml/v2"
)

const ConfigName = "config.toml"

type Config struct {
	// Templates overrides the templates root (default <home>/templates).
	Templates string `toml:"templates,omitempty"`
	// Source is the git URL or path the templates were initialised from.
	Source string `toml:"source,omitempty"`
	// DefaultHarnesses preselects harnesses in the wizard and CLI.
	DefaultHarnesses []string `toml:"default_harnesses,omitempty"`
}

// Home resolves: flag > $OCWS_HOME > $XDG_CONFIG_HOME/ocws > ~/.config/ocws
// (Linux and macOS alike) > %APPDATA%\ocws on Windows.
func Home(flag string) (string, error) {
	if flag != "" {
		return filepath.Abs(engine.ExpandHome(flag))
	}
	if v := os.Getenv("OCWS_HOME"); v != "" {
		return filepath.Abs(engine.ExpandHome(v))
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, "ocws"), nil
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "ocws"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "ocws"), nil
}

func LoadConfig(home string) (Config, error) {
	var c Config
	data, err := os.ReadFile(filepath.Join(home, ConfigName))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", filepath.Join(home, ConfigName), err)
	}
	return c, nil
}

func SaveConfig(home string, c Config) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	header := "# ocws configuration. See `ocws --help`.\n"
	return os.WriteFile(filepath.Join(home, ConfigName), append([]byte(header), data...), 0o644)
}

// TemplatesRoot resolves: flag > $OCWS_TEMPLATES > config.templates > <home>/templates.
// Relative flag/env values are relative to the current directory; relative
// config values are relative to the ocws home.
func TemplatesRoot(home, flag string, c Config) (string, error) {
	for _, v := range []string{flag, os.Getenv("OCWS_TEMPLATES")} {
		if v != "" {
			return filepath.Abs(engine.ExpandHome(v))
		}
	}
	if v := c.Templates; v != "" {
		v = engine.ExpandHome(v)
		if !filepath.IsAbs(v) {
			v = filepath.Join(home, v)
		}
		return filepath.Clean(v), nil
	}
	return filepath.Join(home, "templates"), nil
}
