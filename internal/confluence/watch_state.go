package confluence

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

func confluenceStateDir() string {
	if dir := strings.TrimSpace(os.Getenv("COJIRA_CONFLUENCE_STATE_DIR")); dir != "" {
		return dir
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "cojira", "confluence")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".cache", "cojira", "confluence")
	}
	return filepath.Join(home, ".cache", "cojira", "confluence")
}

func confluenceStatePath(name string) string {
	return filepath.Join(confluenceStateDir(), name)
}

func confluencePollStatePath(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return confluenceStatePath("poll_" + hex.EncodeToString(sum[:8]) + ".json")
}
