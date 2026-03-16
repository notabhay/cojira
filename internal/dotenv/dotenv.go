// Package dotenv provides a minimal .env file parser and loader,
// plus placeholder detection for template values.
package dotenv

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// SourceEnvironment indicates a key came from the inherited process
	// environment rather than a loaded file.
	SourceEnvironment = "environment"
)

// LoadResult describes a single dotenv load attempt.
type LoadResult struct {
	CandidatePaths      []string          `json:"candidate_paths"`
	LoadedPath          string            `json:"loaded_path,omitempty"`
	LoadedPaths         []string          `json:"loaded_paths,omitempty"`
	KeysSet             []string          `json:"keys_set"`
	KeysSkippedExisting []string          `json:"keys_skipped_existing"`
	KeySources          map[string]string `json:"key_sources"`
	LoadErrors          map[string]string `json:"load_errors,omitempty"`
}

var (
	loadStateMu    sync.RWMutex
	lastLoadResult = LoadResult{
		CandidatePaths:      []string{},
		LoadedPaths:         []string{},
		KeysSet:             []string{},
		KeysSkippedExisting: []string{},
		KeySources:          map[string]string{},
		LoadErrors:          map[string]string{},
	}
	keySources = map[string]string{}
)

// CredentialsPath returns the default path for global cojira credentials
// (in .env format). This lets users configure Jira/Confluence once and reuse
// it across workspaces.
//
// Path:
// - $XDG_CONFIG_HOME/cojira/credentials (if set)
// - $HOME/.config/cojira/credentials
func CredentialsPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "cojira", "credentials")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".config", "cojira", "credentials")
}

// ParseLines parses .env file content into key-value pairs.
// It handles comments, blank lines, "export" prefixes, and quoted values.
func ParseLines(content string) map[string]string {
	parsed := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			if len(value) >= 2 {
				value = value[1 : len(value)-1]
			}
		}
		parsed[key] = value
	}
	return parsed
}

// DefaultSearchPaths returns the default .env search paths:
// - the current working directory's .env file
// - the user's global credentials file (~/.config/cojira/credentials)
func DefaultSearchPaths() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	paths := []string{filepath.Join(cwd, ".env")}
	if cred := CredentialsPath(); cred != "" {
		paths = append(paths, cred)
	}
	return paths
}

// LoadIfPresent loads the first existing .env file from paths.
// It sets environment variables that are not already set.
// Returns a detailed result for the load attempt.
func LoadIfPresent(paths []string) LoadResult {
	result := LoadResult{
		CandidatePaths:      append([]string(nil), paths...),
		LoadedPaths:         []string{},
		KeysSet:             []string{},
		KeysSkippedExisting: []string{},
		KeySources:          map[string]string{},
		LoadErrors:          map[string]string{},
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			result.LoadErrors[p] = err.Error()
			continue
		}
		if result.LoadedPath == "" {
			result.LoadedPath = p
		}
		result.LoadedPaths = append(result.LoadedPaths, p)
		values := ParseLines(string(data))
		for key, value := range values {
			if _, exists := os.LookupEnv(key); exists {
				result.KeysSkippedExisting = append(result.KeysSkippedExisting, key)
				source := sourceForExistingKey(key)
				result.KeySources[key] = source
				trackKeySource(key, source)
				continue
			}
			_ = os.Setenv(key, value)
			result.KeysSet = append(result.KeysSet, key)
			result.KeySources[key] = p
			trackKeySource(key, p)
		}
	}
	setLastLoadResult(result)
	return result
}

// placeholders is the set of known template placeholder values.
var placeholders = map[string]struct{}{
	"you@example.com":                 {},
	"your-email@example.com":          {},
	"user@example.com":                {},
	"your.email@example.com":          {},
	"your-personal-access-token-here": {},
	"your-api-token-here":             {},
}

// IsPlaceholder returns true if value looks like a template placeholder.
// The optional field parameter enables a heuristic for email fields.
func IsPlaceholder(value string, field string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	lower := strings.ToLower(v)
	if _, ok := placeholders[lower]; ok {
		return true
	}
	if field != "" && strings.Contains(strings.ToLower(field), "email") {
		return strings.HasPrefix(lower, "your") && strings.Contains(lower, "@example")
	}
	return false
}

// LastLoadResult returns the most recent dotenv load attempt.
func LastLoadResult() LoadResult {
	loadStateMu.RLock()
	defer loadStateMu.RUnlock()
	return cloneLoadResult(lastLoadResult)
}

// SourceForKey returns the tracked source for an environment variable.
// It falls back to "environment" when the key is present but was not set by
// a tracked dotenv load in this process.
func SourceForKey(key string) string {
	loadStateMu.RLock()
	source := keySources[key]
	loadStateMu.RUnlock()
	if source != "" {
		return source
	}
	if _, exists := os.LookupEnv(key); exists {
		return SourceEnvironment
	}
	return ""
}

// Provenance reports whether each key is present and where it came from.
func Provenance(keys []string) map[string]map[string]any {
	report := make(map[string]map[string]any, len(keys))
	for _, key := range keys {
		_, present := os.LookupEnv(key)
		report[key] = map[string]any{
			"present": present,
			"source":  SourceForKey(key),
		}
	}
	return report
}

// ResetTracking clears tracked dotenv state. It exists for tests and any
// workflows that need to explicitly discard previous in-process provenance.
func ResetTracking() {
	loadStateMu.Lock()
	defer loadStateMu.Unlock()
	lastLoadResult = LoadResult{
		CandidatePaths:      []string{},
		LoadedPaths:         []string{},
		KeysSet:             []string{},
		KeysSkippedExisting: []string{},
		KeySources:          map[string]string{},
		LoadErrors:          map[string]string{},
	}
	keySources = map[string]string{}
}

func trackKeySource(key, source string) {
	if key == "" || source == "" {
		return
	}
	loadStateMu.Lock()
	defer loadStateMu.Unlock()
	if keySources == nil {
		keySources = map[string]string{}
	}
	keySources[key] = source
}

func sourceForExistingKey(key string) string {
	loadStateMu.RLock()
	source := keySources[key]
	loadStateMu.RUnlock()
	if source != "" {
		return source
	}
	return SourceEnvironment
}

func setLastLoadResult(result LoadResult) {
	loadStateMu.Lock()
	defer loadStateMu.Unlock()
	lastLoadResult = cloneLoadResult(result)
}

func cloneLoadResult(result LoadResult) LoadResult {
	cp := LoadResult{
		CandidatePaths:      append([]string(nil), result.CandidatePaths...),
		LoadedPath:          result.LoadedPath,
		LoadedPaths:         append([]string(nil), result.LoadedPaths...),
		KeysSet:             append([]string(nil), result.KeysSet...),
		KeysSkippedExisting: append([]string(nil), result.KeysSkippedExisting...),
		KeySources:          map[string]string{},
		LoadErrors:          map[string]string{},
	}
	for key, source := range result.KeySources {
		cp.KeySources[key] = source
	}
	for path, msg := range result.LoadErrors {
		cp.LoadErrors[path] = msg
	}
	return cp
}
