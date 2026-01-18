// Package config handles application configuration loading from YAML files.
// Supports environment variable expansion in string values.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	Telegram    TelegramConfig `yaml:"telegram"`
	CommandsDir string         `yaml:"commands_dir"`
	PluginsDir  string         `yaml:"plugins_dir"`
	Database    DatabaseConfig `yaml:"database"`
	Defaults    DefaultsConfig `yaml:"defaults"`
	Podcast     PodcastConfig  `yaml:"podcast"`
}

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	Token          string  `yaml:"token"`
	AllowedChatIDs []int64 `yaml:"allowed_chat_ids"`
}

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// DefaultsConfig holds default values for command execution.
type DefaultsConfig struct {
	Timeout   time.Duration `yaml:"timeout"`
	MaxOutput int           `yaml:"max_output"`
}

// PodcastConfig holds configuration for podcast generation.
type PodcastConfig struct {
	PodcastgenPath string `yaml:"podcastgen_path"` // Path to podcastgen directory
	ConfigPath     string `yaml:"config_path"`     // Path to TTS config.yml
	TempDir        string `yaml:"temp_dir"`        // Temp directory for files
}

// Load reads configuration from the specified YAML file path.
// Supports ${ENV_VAR} expansion in string values.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.setDefaults(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// setDefaults applies default values for unset fields.
func (c *Config) setDefaults() error {
	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required")
	}

	if len(c.Telegram.AllowedChatIDs) == 0 {
		return fmt.Errorf("telegram.allowed_chat_ids must have at least one entry")
	}

	if c.CommandsDir == "" {
		c.CommandsDir = "./commands"
	}

	if c.Database.Path == "" {
		c.Database.Path = "./audit.db"
	}

	if c.Defaults.Timeout == 0 {
		c.Defaults.Timeout = 60 * time.Second
	}

	if c.Defaults.MaxOutput == 0 {
		c.Defaults.MaxOutput = 5000
	}

	return nil
}

// ExpandPath resolves a path relative to the config file directory.
func (c *Config) ExpandPath(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(filepath.Dir(base), path)
}

// envVarPattern matches ${VAR} or $VAR patterns.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// expandEnvVars replaces ${VAR} and $VAR with environment variable values.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		var name string
		if match[1] == '{' {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}
