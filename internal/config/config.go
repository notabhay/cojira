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

// GetStringMap returns a named nested object as a string map. Non-string values
// are skipped.
func (c *ProjectConfig) GetStringMap(keys []string) map[string]string {
	raw := c.GetValue(keys, nil)
	m, ok := raw.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for key, value := range m {
		s, ok := value.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		out[key] = strings.TrimSpace(s)
	}
	return out
}

// GetObject returns a nested object value or an empty map when absent.
func (c *ProjectConfig) GetObject(keys []string) map[string]any {
	raw := c.GetValue(keys, nil)
	m, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
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

func parseConfigFile(configPath string) (*ProjectConfig, error) {
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

func discoverConfigPaths() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	var dirs []string
	for cur := cwd; ; cur = filepath.Dir(cur) {
		dirs = append(dirs, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}

	var paths []string
	for i := len(dirs) - 1; i >= 0; i-- {
		paths = append(paths, filepath.Join(dirs[i], ConfigFilename))
	}
	return paths
}

// NearestConfigPath returns the closest existing .cojira.json walking from cwd
// to the filesystem root. If no config exists, it returns the path that should
// be created in the cwd.
func NearestConfigPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ConfigFilename
	}
	for cur := cwd; ; cur = filepath.Dir(cur) {
		candidate := filepath.Join(cur, ConfigFilename)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return filepath.Join(cwd, ConfigFilename)
}

// LoadWritableProjectConfig loads the closest writable project config. If no
// file exists yet, it returns an empty config rooted at the cwd path.
func LoadWritableProjectConfig() (*ProjectConfig, error) {
	path := NearestConfigPath()
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return &ProjectConfig{
			Path: path,
			Data: map[string]any{},
		}, nil
	}
	return parseConfigFile(path)
}

// WriteProjectConfig writes a project config as indented JSON.
func WriteProjectConfig(path string, data map[string]any) error {
	if strings.TrimSpace(path) == "" {
		return &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     "Config path is required.",
			UserMessage: fmt.Sprintf("I couldn't determine where to write %s.", ConfigFilename),
			ExitCode:    2,
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Unable to encode %s: %v", path, err),
			UserMessage: fmt.Sprintf("I couldn't save %s because the generated JSON was invalid.", ConfigFilename),
			ExitCode:    2,
		}
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

func mergeValue(dst any, src any) any {
	dstMap, dstOK := dst.(map[string]any)
	srcMap, srcOK := src.(map[string]any)
	if !dstOK || !srcOK {
		return src
	}

	merged := make(map[string]any, len(dstMap))
	for k, v := range dstMap {
		merged[k] = v
	}
	for k, v := range srcMap {
		if existing, ok := merged[k]; ok {
			merged[k] = mergeValue(existing, v)
			continue
		}
		merged[k] = v
	}
	return merged
}

// LoadProjectConfig searches for .cojira.json in the given paths (or default
// paths if nil) and loads the first one found.
// Returns (nil, nil) if no config file exists.
func LoadProjectConfig(paths []string) (*ProjectConfig, error) {
	if paths == nil {
		paths = discoverConfigPaths()
		var merged map[string]any
		nearestPath := ""
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			cfg, err := parseConfigFile(p)
			if err != nil {
				return nil, err
			}
			if merged == nil {
				merged = map[string]any{}
			}
			merged = mergeValue(merged, cfg.Data).(map[string]any)
			nearestPath = p
		}
		if merged == nil {
			return nil, nil
		}
		return &ProjectConfig{Path: nearestPath, Data: merged}, nil
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			return parseConfigFile(p)
		}
	}
	return nil, nil
}
