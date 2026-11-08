// Package config loads and stores the CLI configuration file.
//
// Precedence (highest first): command-line flag > environment variable > config file > default.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// Config is the on-disk configuration.
type Config struct {
	Token        string `json:"token,omitempty"`
	SpaceID      string `json:"space_id,omitempty"`
	Language     string `json:"language,omitempty"`
	Days         int    `json:"days,omitempty"`
	MaxDownloads int    `json:"max_downloads,omitempty"`
	Parallel     int    `json:"parallel,omitempty"`
	// EmailValidations caches confirmed sender addresses for anonymous sends (email -> id).
	EmailValidations map[string]string `json:"email_validations,omitempty"`
}

// Keys lists the keys accepted by `config set`.
var Keys = []string{"token", "space_id", "language", "days", "max_downloads", "parallel"}

// DefaultPath returns the config file path for the current OS.
//
//	Linux/macOS: $XDG_CONFIG_HOME/swisstransfer/config.json or ~/.config/swisstransfer/config.json
//	Windows:     %APPDATA%\swisstransfer\config.json
func DefaultPath() (string, error) {
	if p := os.Getenv("SWISSTRANSFER_CONFIG"); p != "" {
		return p, nil
	}
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("APPDATA")
		if base == "" {
			d, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			base = d
		}
	} else {
		base = os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "swisstransfer", "config.json"), nil
}

// Load reads the config file. A missing file yields an empty config and no error.
func Load(path string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

// Save writes the config with 0600 permissions, creating parent directories.
func Save(path string, c *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	// Rename keeps the temp file's mode; enforce on platforms where it matters.
	_ = os.Chmod(path, 0o600)
	return nil
}

// Set assigns a key from its string form, validating the value.
func (c *Config) Set(key, value string) error {
	switch key {
	case "token":
		c.Token = value
	case "space_id":
		if value != "" {
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf("space_id must be an integer")
			}
		}
		c.SpaceID = value
	case "language":
		c.Language = value
	case "days", "max_downloads", "parallel":
		n := 0
		if value != "" {
			v, err := strconv.Atoi(value)
			if err != nil || v < 0 {
				return fmt.Errorf("%s must be a positive integer", key)
			}
			n = v
		}
		switch key {
		case "days":
			c.Days = n
		case "max_downloads":
			c.MaxDownloads = n
		case "parallel":
			c.Parallel = n
		}
	default:
		return fmt.Errorf("unknown key %q (valid: %v)", key, Keys)
	}
	return nil
}

// Get returns the string form of a key.
func (c *Config) Get(key string) (string, error) {
	switch key {
	case "token":
		return c.Token, nil
	case "space_id":
		return c.SpaceID, nil
	case "language":
		return c.Language, nil
	case "days":
		return itoa(c.Days), nil
	case "max_downloads":
		return itoa(c.MaxDownloads), nil
	case "parallel":
		return itoa(c.Parallel), nil
	}
	return "", fmt.Errorf("unknown key %q (valid: %v)", key, Keys)
}

func itoa(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// MaskToken shows only the last 4 characters of a secret.
func MaskToken(t string) string {
	if t == "" {
		return ""
	}
	if len(t) <= 8 {
		return "********"
	}
	return "********" + t[len(t)-4:]
}
