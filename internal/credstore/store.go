package credstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/config"
	"github.com/zalando/go-keyring"
)

const (
	StorePlain   = "plain"
	StoreKeyring = "keyring"
	StoreAuto    = "auto"

	keyringService = "cojira"
	keyringUser    = "credentials"
)

var supportedKeys = []string{
	"CONFLUENCE_API_TOKEN",
	"CONFLUENCE_AUTH_MODE",
	"CONFLUENCE_BASE_URL",
	"CONFLUENCE_OAUTH_ACCESS_TOKEN",
	"CONFLUENCE_OAUTH_CLIENT_ID",
	"CONFLUENCE_OAUTH_CLIENT_SECRET",
	"CONFLUENCE_OAUTH_CLOUD_ID",
	"CONFLUENCE_OAUTH_EXPIRY",
	"CONFLUENCE_OAUTH_REFRESH_TOKEN",
	"CONFLUENCE_OAUTH_TOKEN_URL",
	"JIRA_API_TOKEN",
	"JIRA_AUTH_MODE",
	"JIRA_BASE_URL",
	"JIRA_EMAIL",
	"JIRA_OAUTH_ACCESS_TOKEN",
	"JIRA_OAUTH_CLIENT_ID",
	"JIRA_OAUTH_CLIENT_SECRET",
	"JIRA_OAUTH_CLOUD_ID",
	"JIRA_OAUTH_EXPIRY",
	"JIRA_OAUTH_REFRESH_TOKEN",
	"JIRA_OAUTH_TOKEN_URL",
}

// ResolveStoreName resolves the effective credential store from env or config.
func ResolveStoreName() string {
	if env := normalizeStoreName(os.Getenv("COJIRA_CRED_STORE")); env != "" {
		return env
	}
	cfg, err := config.LoadProjectConfig(nil)
	if err == nil && cfg != nil {
		if value, ok := cfg.GetValue([]string{"credential_store"}, "").(string); ok && strings.TrimSpace(value) != "" {
			if name := normalizeStoreName(value); name != "" {
				return name
			}
		}
		if value, ok := cfg.GetValue([]string{"auth", "credential_store"}, "").(string); ok && strings.TrimSpace(value) != "" {
			if name := normalizeStoreName(value); name != "" {
				return name
			}
		}
	}
	return StorePlain
}

func normalizeStoreName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", StorePlain:
		return StorePlain
	case StoreAuto:
		return StoreAuto
	case "keychain", "keyring", "secure":
		return StoreKeyring
	default:
		return ""
	}
}

// NormalizeStoreNamePublic exposes store-name normalization for command parsing.
func NormalizeStoreNamePublic(value string) string {
	return normalizeStoreName(value)
}

// EffectiveStoreName resolves auto to the actual backend used.
func EffectiveStoreName() string {
	switch ResolveStoreName() {
	case StoreAuto:
		if KeyringAvailable() {
			return StoreKeyring
		}
		return StorePlain
	case StoreKeyring:
		if KeyringAvailable() {
			return StoreKeyring
		}
		return StorePlain
	default:
		return StorePlain
	}
}

// KeyringAvailable reports whether the current platform can attempt keyring usage.
func KeyringAvailable() bool {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return true
	default:
		return false
	}
}

// Load returns credentials from the active credential store.
func Load() (map[string]string, string, error) {
	store := EffectiveStoreName()
	switch store {
	case StoreKeyring:
		values, err := loadKeyring()
		return values, store, err
	default:
		return map[string]string{}, store, nil
	}
}

// SaveToKeyring writes credentials to the secure keyring backend.
func SaveToKeyring(values map[string]string) error {
	payload, err := marshal(values)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, keyringUser, payload)
}

// DeleteFromKeyring removes stored credentials from keyring.
func DeleteFromKeyring() error {
	if err := keyring.Delete(keyringService, keyringUser); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		return err
	}
	return nil
}

// KeyringStatus reports whether credentials exist in keyring.
func KeyringStatus() (bool, error) {
	_, err := keyring.Get(keyringService, keyringUser)
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return false, nil
	}
	return false, err
}

// ParseKnownEnv extracts supported credential keys from a larger env-style map.
func ParseKnownEnv(values map[string]string) map[string]string {
	out := map[string]string{}
	for _, key := range supportedKeys {
		if value := strings.TrimSpace(values[key]); value != "" {
			out[key] = value
		}
	}
	return out
}

// ParseKnownProcessEnv extracts supported credential keys from the current process env.
func ParseKnownProcessEnv() map[string]string {
	out := map[string]string{}
	for _, key := range supportedKeys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			out[key] = value
		}
	}
	return out
}

// WritePlainCredentials writes supported credentials to the global plaintext credentials file.
func WritePlainCredentials(values map[string]string) (string, error) {
	path := credentialsPath()
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("credentials path is unavailable")
	}
	values = ParseKnownEnv(values)
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(formatEnv(values)), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// HasPlainCredentials reports whether the plaintext credentials file exists.
func HasPlainCredentials() (bool, string) {
	path := credentialsPath()
	if strings.TrimSpace(path) == "" {
		return false, ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false, path
	}
	return true, path
}

func loadKeyring() (map[string]string, error) {
	raw, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return ParseKnownEnv(decoded), nil
	}
	return ParseKnownEnv(parseEnvLines(raw)), nil
}

func marshal(values map[string]string) (string, error) {
	values = ParseKnownEnv(values)
	ordered := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ordered[key] = values[key]
	}
	data, err := json.Marshal(ordered)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatEnv(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(values[key])
		b.WriteString("\n")
	}
	return b.String()
}

func filepathDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "/"
	}
	return path[:idx]
}

func credentialsPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "cojira", "credentials")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".config", "cojira", "credentials")
}

func parseEnvLines(content string) map[string]string {
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
