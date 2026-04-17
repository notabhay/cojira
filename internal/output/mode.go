package output

import (
	"os"
	"sync"

	"golang.org/x/term"
)

var (
	modeMu    sync.RWMutex
	mode      string // empty means "not set"
	colorMode string // empty means "not set"
)

// SetMode sets the global output mode (e.g. "human", "json", "summary").
// Pass "" to clear the override so GetMode falls back to the environment.
func SetMode(m string) {
	modeMu.Lock()
	mode = m
	modeMu.Unlock()
}

// SetColorMode sets the global color mode: auto, always, or never.
func SetColorMode(m string) {
	modeMu.Lock()
	colorMode = m
	modeMu.Unlock()
}

// GetMode returns the current output mode.
// Priority: explicit SetMode > COJIRA_OUTPUT_MODE env > "human".
func GetMode() string {
	modeMu.RLock()
	m := mode
	modeMu.RUnlock()
	if m != "" {
		return m
	}
	if env := os.Getenv("COJIRA_OUTPUT_MODE"); env != "" {
		return env
	}
	return "human"
}

// GetColorMode returns the current color mode.
func GetColorMode() string {
	modeMu.RLock()
	m := colorMode
	modeMu.RUnlock()
	if m != "" {
		return m
	}
	if env := os.Getenv("COJIRA_COLOR"); env != "" {
		return env
	}
	return "auto"
}

// ShouldColorize returns true when human output should use ANSI colors.
func ShouldColorize() bool {
	switch GetColorMode() {
	case "always":
		return true
	case "never":
		return false
	default:
		return IsTTY(int(os.Stdout.Fd()))
	}
}

// IsTTY reports whether the given file descriptor is a terminal.
func IsTTY(fd int) bool {
	return term.IsTerminal(fd)
}
