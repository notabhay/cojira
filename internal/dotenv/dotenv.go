// Package dotenv provides a minimal .env file parser and loader,
// plus placeholder detection for template values.
package dotenv

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

var (
	loadOnceMu    sync.Mutex
	loadOnceCache = map[string]string{}
)

// LoadIfPresent loads every existing .env file from paths in order.
// It sets environment variables that are not already set, so earlier files
// win over later files and process env always has highest precedence.
// Returns the first path that was successfully loaded, or empty string if none
// were loaded.
func LoadIfPresent(paths []string) string {
	firstLoaded := ""
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		values := ParseLines(string(data))
		for key, value := range values {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
			_ = os.Setenv(key, value)
		}
		if firstLoaded == "" {
			firstLoaded = p
		}
	}
	return firstLoaded
}

// LoadIfPresentOnce loads the given paths at most once per process for the
// exact ordered path set. It is useful for startup helpers that may be reached
// through multiple code paths in the same command execution.
func LoadIfPresentOnce(paths []string) string {
	key := strings.Join(paths, "\x00")

	loadOnceMu.Lock()
	loaded, ok := loadOnceCache[key]
	loadOnceMu.Unlock()
	if ok {
		return loaded
	}

	loaded = LoadIfPresent(paths)

	loadOnceMu.Lock()
	loadOnceCache[key] = loaded
	loadOnceMu.Unlock()
	return loaded
}

// LoadDefaultOnce loads the default search paths at most once per process.
func LoadDefaultOnce() string {
	return LoadIfPresentOnce(DefaultSearchPaths())
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
