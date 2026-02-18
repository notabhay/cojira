// Package version exposes the cojira version string.
package version

// Version is the current cojira version. It matches the Python
// package's __version__ and can be overridden at build time via
// -ldflags "-X github.com/cojira/cojira/internal/version.Version=x.y.z".
var Version = "0.1.0"
