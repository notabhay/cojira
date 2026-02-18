// Package assets embeds the bootstrap template files shipped with cojira.
package assets

import "embed"

// FS holds all embedded bootstrap assets:
//   - COJIRA-BOOTSTRAP.md
//   - env.example
//   - examples/*
//
//go:embed COJIRA-BOOTSTRAP.md env.example examples/*
var FS embed.FS
