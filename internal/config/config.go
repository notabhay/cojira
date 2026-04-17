package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	ExitStatus  int
}

func (e *ConfigError) Error() string {
	return e.Message
}

// ExitCode returns the process exit code associated with the config error.
func (e *ConfigError) ExitCode() int {
	return e.ExitStatus
}

// ProjectConfig holds the parsed project configuration from .cojira.json.
type ProjectConfig struct {
	Path string
	Data map[string]any
}

// Profile represents a named profile loaded from .cojira.json.
type Profile struct {
	Name string
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
			ExitStatus:  2,
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
			ExitStatus:  2,
		}
	}

	if strings.TrimSpace(string(raw)) == "" {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("%s is empty.", configPath),
			UserMessage: fmt.Sprintf("Your %s file is empty.", ConfigFilename),
			ExitStatus:  2,
		}
	}

	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Invalid JSON in %s: %v", configPath, err),
			UserMessage: fmt.Sprintf("Your %s file isn't valid JSON.", ConfigFilename),
			ExitStatus:  2,
		}
	}

	m, ok := data.(map[string]any)
	if !ok {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("%s JSON root must be an object.", configPath),
			UserMessage: fmt.Sprintf("Your %s file must contain a JSON object at the top level.", ConfigFilename),
			ExitStatus:  2,
		}
	}

	for _, section := range []string{"jira", "confluence", "aliases", "profiles"} {
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
			ExitStatus:  2,
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
			ExitStatus:  2,
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

var profileEnvKeyMap = map[string]string{
	"jira.base_url":                  "JIRA_BASE_URL",
	"jira.api_token":                 "JIRA_API_TOKEN",
	"jira.email":                     "JIRA_EMAIL",
	"jira.project":                   "JIRA_PROJECT",
	"jira.api_version":               "JIRA_API_VERSION",
	"jira.auth_mode":                 "JIRA_AUTH_MODE",
	"jira.verify_ssl":                "JIRA_VERIFY_SSL",
	"jira.user_agent":                "JIRA_USER_AGENT",
	"jira.oauth_access_token":        "JIRA_OAUTH_ACCESS_TOKEN",
	"jira.oauth_refresh_token":       "JIRA_OAUTH_REFRESH_TOKEN",
	"jira.oauth_client_id":           "JIRA_OAUTH_CLIENT_ID",
	"jira.oauth_client_secret":       "JIRA_OAUTH_CLIENT_SECRET",
	"jira.oauth_token_url":           "JIRA_OAUTH_TOKEN_URL",
	"jira.oauth_cloud_id":            "JIRA_OAUTH_CLOUD_ID",
	"jira.oauth_expiry":              "JIRA_OAUTH_EXPIRY",
	"confluence.base_url":            "CONFLUENCE_BASE_URL",
	"confluence.api_token":           "CONFLUENCE_API_TOKEN",
	"confluence.api_version":         "CONFLUENCE_API_VERSION",
	"confluence.auth_mode":           "CONFLUENCE_AUTH_MODE",
	"confluence.verify_ssl":          "CONFLUENCE_VERIFY_SSL",
	"confluence.user_agent":          "CONFLUENCE_USER_AGENT",
	"confluence.oauth_access_token":  "CONFLUENCE_OAUTH_ACCESS_TOKEN",
	"confluence.oauth_refresh_token": "CONFLUENCE_OAUTH_REFRESH_TOKEN",
	"confluence.oauth_client_id":     "CONFLUENCE_OAUTH_CLIENT_ID",
	"confluence.oauth_client_secret": "CONFLUENCE_OAUTH_CLIENT_SECRET",
	"confluence.oauth_token_url":     "CONFLUENCE_OAUTH_TOKEN_URL",
	"confluence.oauth_cloud_id":      "CONFLUENCE_OAUTH_CLOUD_ID",
	"confluence.oauth_expiry":        "CONFLUENCE_OAUTH_EXPIRY",
}

func resolveProfileName(cfg *ProjectConfig, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	if env := strings.TrimSpace(os.Getenv("COJIRA_PROFILE")); env != "" {
		return env
	}
	if cfg == nil {
		return ""
	}
	if value, ok := cfg.GetValue([]string{"default_profile"}, "").(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// ResolveProfileName resolves the effective profile name from explicit input,
// environment, or the current project config default_profile value.
func ResolveProfileName(requested string) (string, error) {
	cfg, err := LoadProjectConfig(nil)
	if err != nil {
		return "", err
	}
	return resolveProfileName(cfg, requested), nil
}

// LoadProfile loads a named profile from the merged project config. When
// requested is empty it falls back to COJIRA_PROFILE and default_profile.
func LoadProfile(requested string) (*Profile, error) {
	cfg, err := LoadProjectConfig(nil)
	if err != nil {
		return nil, err
	}
	name := resolveProfileName(cfg, requested)
	if name == "" {
		return nil, nil
	}
	if cfg == nil {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Profile %q was requested but no %s file was found.", name, ConfigFilename),
			UserMessage: fmt.Sprintf("I couldn't find profile %q because %s is missing.", name, ConfigFilename),
			ExitStatus:  2,
		}
	}
	profiles := cfg.GetSection("profiles")
	raw, ok := profiles[name]
	if !ok {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Profile %q was not found in %s.", name, cfg.Path),
			UserMessage: fmt.Sprintf("Your %s file does not define profile %q.", ConfigFilename, name),
			ExitStatus:  2,
		}
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, &ConfigError{
			Code:        CodeConfigInvalid,
			Message:     fmt.Sprintf("Profile %q in %s must be an object.", name, cfg.Path),
			UserMessage: fmt.Sprintf("Profile %q in %s is invalid.", name, ConfigFilename),
			ExitStatus:  2,
		}
	}
	return &Profile{Name: name, Data: data}, nil
}

// ListProfileNames returns the sorted profile names defined in the merged
// project config.
func ListProfileNames() ([]string, error) {
	cfg, err := LoadProjectConfig(nil)
	if err != nil || cfg == nil {
		return nil, err
	}
	profiles := cfg.GetSection("profiles")
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func coerceProfileEnvValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strings.TrimSpace(strings.TrimSuffix(fmt.Sprintf("%.0f", typed), ".0"))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func addStructuredProfileEnv(overrides map[string]string, prefix string, values map[string]any) {
	for key, raw := range values {
		envKey, ok := profileEnvKeyMap[prefix+"."+key]
		if !ok {
			continue
		}
		value := coerceProfileEnvValue(raw)
		if value == "" {
			continue
		}
		overrides[envKey] = value
	}
}

// ProfileEnvOverrides converts the selected profile into environment-style
// overrides. Direct env entries under profiles.<name>.env override structured
// jira/confluence values.
func ProfileEnvOverrides(requested string) (map[string]string, string, error) {
	profile, err := LoadProfile(requested)
	if err != nil || profile == nil {
		return map[string]string{}, "", err
	}
	overrides := map[string]string{}
	if jiraData, ok := profile.Data["jira"].(map[string]any); ok {
		addStructuredProfileEnv(overrides, "jira", jiraData)
	}
	if confData, ok := profile.Data["confluence"].(map[string]any); ok {
		addStructuredProfileEnv(overrides, "confluence", confData)
	}
	if envData, ok := profile.Data["env"].(map[string]any); ok {
		for key, raw := range envData {
			value := coerceProfileEnvValue(raw)
			if strings.TrimSpace(key) == "" || value == "" {
				continue
			}
			overrides[strings.TrimSpace(key)] = value
		}
	}
	return overrides, profile.Name, nil
}
