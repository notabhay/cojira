// Package dotenv provides a minimal .env file parser and loader,
// plus placeholder detection for template values.
package dotenv

import (
	"os"
	"path/filepath"
	"strings"
)

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
// the current working directory's .env file.
func DefaultSearchPaths() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(cwd, ".env")}
}

// LoadIfPresent loads the first existing .env file from paths.
// It sets environment variables that are not already set.
// Returns the path of the loaded file, or empty string if none was loaded.
func LoadIfPresent(paths []string) string {
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
		return p
	}
	return ""
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
