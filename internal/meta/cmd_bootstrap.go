package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/notabhay/cojira/internal/assets"
	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

const (
	workspacePromptBlockStart = "<!-- COJIRA:BEGIN -->"
	workspacePromptBlockEnd   = "<!-- COJIRA:END -->"
)

// readAsset reads an embedded asset from the assets FS.
func readAsset(name string) (string, error) {
	data, err := assets.FS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("embedded asset %q: %w", name, err)
	}
	return string(data), nil
}

// writeTextFile writes content to path, creating parent dirs.
// Returns "written" if the file was created, "skipped" if it already existed
// and force is false.
func writeTextFile(path string, content string, force bool) (string, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !force {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("path exists and is not a file: %s", path)
			}
			return "skipped", nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "written", nil
}

func writeWorkspacePromptFile(path string, content string) (string, error) {
	block := workspacePromptBlockStart + "\n" + strings.TrimSpace(content) + "\n" + workspacePromptBlockEnd + "\n"

	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(block), 0o644); err != nil {
			return "", err
		}
		return "written", nil
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path exists and is not a file: %s", path)
	}

	existingBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	existing := string(existingBytes)

	start := strings.Index(existing, workspacePromptBlockStart)
	end := strings.Index(existing, workspacePromptBlockEnd)
	if start >= 0 && end >= start {
		end += len(workspacePromptBlockEnd)
		updated := existing[:start] + block + existing[end:]
		if updated == existing {
			return "skipped", nil
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return "merged", nil
	}

	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		trimmed = block[:len(block)-1]
	} else {
		trimmed = trimmed + "\n\n" + strings.TrimRight(block, "\n")
	}
	updated := trimmed + "\n"
	if updated == existing {
		return "skipped", nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return "merged", nil
}

// NewBootstrapCmd returns the "cojira bootstrap" command.
func NewBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "bootstrap",
		Short:         "Merge cojira workspace guidance into AGENTS.md and CLAUDE.md",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runBootstrap,
	}
	cli.AddOutputFlags(cmd, false)
	cmd.Flags().String("dir", ".", "Directory where AGENTS.md and CLAUDE.md should be merged")
	return cmd
}

func runBootstrap(cmd *cobra.Command, _ []string) error {
	cli.NormalizeOutputMode(cmd)
	jsonOut := cli.IsJSON(cmd)
	targetDir, _ := cmd.Flags().GetString("dir")
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		targetDir = "."
	}
	targetDir = filepath.Clean(targetDir)

	type writeItem struct{ src, dst string }
	writes := []writeItem{
		{"workspace/AGENTS.md", filepath.Join(targetDir, "AGENTS.md")},
		{"workspace/AGENTS.md", filepath.Join(targetDir, "CLAUDE.md")},
	}

	wroteAny := false
	var results []map[string]string

	for _, w := range writes {
		content, readErr := readAsset(w.src)
		if readErr != nil {
			if jsonOut {
				errObj, _ := output.ErrorObj(cerrors.OpFailed,
					fmt.Sprintf("Error writing %s: %v", w.dst, readErr),
					"", "", nil)
				env := output.BuildEnvelope(
					false, "cojira", "bootstrap",
					map[string]any{"path": w.dst},
					nil, nil, []any{errObj}, "", "", "", nil,
				)
				_ = output.PrintJSON(env)
				return &exitError{Code: 1}
			}
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", w.dst, readErr)
			return &exitError{Code: 1}
		}

		var status string
		var writeErr error
		status, writeErr = writeWorkspacePromptFile(w.dst, content)
		if writeErr != nil {
			if jsonOut {
				errObj, _ := output.ErrorObj(cerrors.OpFailed,
					fmt.Sprintf("Error writing %s: %v", w.dst, writeErr),
					"", "", nil)
				env := output.BuildEnvelope(
					false, "cojira", "bootstrap",
					map[string]any{"path": w.dst},
					nil, nil, []any{errObj}, "", "", "", nil,
				)
				_ = output.PrintJSON(env)
				return &exitError{Code: 1}
			}
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", w.dst, writeErr)
			return &exitError{Code: 1}
		}

		if status == "written" || status == "merged" {
			wroteAny = true
			if !jsonOut {
				label := "wrote"
				if status == "merged" {
					label = "merged"
				}
				fmt.Printf("[%s] %s\n", label, w.dst)
			}
		} else if !jsonOut {
			fmt.Printf("[skip]  %s (already exists)\n", w.dst)
		}
		results = append(results, map[string]string{"path": w.dst, "status": status})
	}

	if jsonOut {
		env := output.BuildEnvelope(
			true, "cojira", "bootstrap",
			map[string]any{"dir": targetDir},
			map[string]any{"items": results, "wrote_any": wroteAny},
			nil, nil, "", "", "", nil,
		)
		return output.PrintJSON(env)
	}

	if !wroteAny {
		fmt.Println("Nothing to do (all files already exist).")
	}
	return nil
}
