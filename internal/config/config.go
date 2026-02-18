package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigFilename is the name of the project config file.
const ConfigFilename = ".cojira.json"

// CodeConfigInvalid is the error code for invalid config files.
const CodeConfigInvalid = "CONFIG_INVALID"

// ConfigError represents a configuration error with a machine-readable code.
type ConfigError struct {
	Code        string
	Message     string
	UserMessage string
	ExitCode    int
}

func (e *ConfigError) Error() string {
	return e.Message
}

// ProjectConfig holds the parsed project configuration from .cojira.json.
type ProjectConfig struct {
	Path string
	Data map[string]any
}

// GetSection returns a named top-level section as a map.
// Returns an empty map if the section doesn't exist or isn't a map.
func (c *ProjectConfig) GetSection(name string) map[string]any {
	v, ok := c.Data[name]
	if !ok {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// GetValue traverses nested keys and returns the value found.
// Returns defaultVal if any key is missing or the intermediate value isn't a map.
func (c *ProjectConfig) GetValue(keys []string, defaultVal any) any {
	var cur any = c.Data
	for _, key := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return defaultVal
		}
		cur, ok = m[key]
		if !ok {
			return defaultVal
		}
	}
	return cur
}

// GetAlias returns the alias command string for name, or empty string if not found.
func (c *ProjectConfig) GetAlias(name string) string {
	aliases := c.GetSection("aliases")
	v, ok := aliases[name]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(s)
}

func coerceMapping(data map[string]any, name string, path string) error {
	v, ok := data[name]
	if !ok || v == nil {
		return nil
	}
	if _, ok := v.(map[string]any); !ok {
		return &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("%s has invalid %s section (expected an object).", path, name),
			UserMessage: fmt.Sprintf("Your %s file has an invalid '%s' section.", ConfigFilename, name),
			ExitCode:    2,
		}
	}
	return nil
}

// DefaultConfigPaths returns the default config file search paths.
func DefaultConfigPaths() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(cwd, ConfigFilename)}
}

// LoadProjectConfig searches for .cojira.json in the given paths (or default
// paths if nil) and loads the first one found.
// Returns (nil, nil) if no config file exists.
func LoadProjectConfig(paths []string) (*ProjectConfig, error) {
	if paths == nil {
		paths = DefaultConfigPaths()
	}

	var configPath string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			configPath = p
			break
		}
	}
	if configPath == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Unable to read %s: %v", configPath, err),
			UserMessage: fmt.Sprintf("I couldn't read %s. Check file permissions.", ConfigFilename),
			ExitCode:    2,
		}
	}

	if strings.TrimSpace(string(raw)) == "" {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("%s is empty.", configPath),
			UserMessage: fmt.Sprintf("Your %s file is empty.", ConfigFilename),
			ExitCode:    2,
		}
	}

	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Invalid JSON in %s: %v", configPath, err),
			UserMessage: fmt.Sprintf("Your %s file isn't valid JSON.", ConfigFilename),
			ExitCode:    2,
		}
	}

	m, ok := data.(map[string]any)
	if !ok {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("%s JSON root must be an object.", configPath),
			UserMessage: fmt.Sprintf("Your %s file must contain a JSON object at the top level.", ConfigFilename),
			ExitCode:    2,
		}
	}

	for _, section := range []string{"jira", "confluence", "aliases"} {
		if err := coerceMapping(m, section, configPath); err != nil {
			return nil, err
		}
	}

	return &ProjectConfig{Path: configPath, Data: m}, nil
}
