package meta

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cojira/cojira/internal/assets"
	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// bootstrapAssets is the list of (embedded path, relative output path) pairs.
var bootstrapAssets = []struct{ src, dst string }{
	{"COJIRA-BOOTSTRAP.md", "COJIRA-BOOTSTRAP.md"},
	{"env.example", ".env.example"},
	{"examples/README.md", "examples/README.md"},
	{"examples/confluence-batch-config.json", "examples/confluence-batch-config.json"},
	{"examples/confluence-page-content.html", "examples/confluence-page-content.html"},
	{"examples/jira-batch-config.json", "examples/jira-batch-config.json"},
	{"examples/jira-bulk-summaries.csv", "examples/jira-bulk-summaries.csv"},
	{"examples/jira-bulk-summaries.json", "examples/jira-bulk-summaries.json"},
	{"examples/jira-create-payload.json", "examples/jira-create-payload.json"},
	{"examples/jira-update-payload.json", "examples/jira-update-payload.json"},
	{"examples/cojira-project.json", "examples/cojira-project.json"},
}

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

// NewBootstrapCmd returns the "cojira bootstrap" command.
func NewBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "bootstrap",
		Short:         "Write COJIRA-BOOTSTRAP.md, .env.example, and example templates into a directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runBootstrap,
	}
	cli.AddOutputFlags(cmd, false)
	cmd.Flags().String("output", "COJIRA-BOOTSTRAP.md", "Path for the bootstrap markdown file")
	cmd.Flags().Bool("no-examples", false, "Do not write .env.example or examples/")
	cmd.Flags().Bool("force", false, "Overwrite existing files")
	cmd.Flags().Bool("stdout", false, "Print the bootstrap markdown to stdout (no files written)")
	return cmd
}

func runBootstrap(cmd *cobra.Command, _ []string) error {
	cli.NormalizeOutputMode(cmd)

	bootstrapMD, err := readAsset("COJIRA-BOOTSTRAP.md")
	if err != nil {
		return err
	}

	jsonOut := cli.IsJSON(cmd)
	stdout, _ := cmd.Flags().GetBool("stdout")
	if stdout {
		if jsonOut {
			env := output.BuildEnvelope(
				true, "cojira", "bootstrap",
				map[string]any{}, map[string]any{"stdout": bootstrapMD},
				nil, nil, "", "", "", nil,
			)
			return output.PrintJSON(env)
		}
		fmt.Print(bootstrapMD)
		return nil
	}

	outputPath, _ := cmd.Flags().GetString("output")
	noExamples, _ := cmd.Flags().GetBool("no-examples")
	force, _ := cmd.Flags().GetBool("force")

	targetDir := filepath.Dir(outputPath)
	if targetDir == "" || targetDir == "." {
		targetDir = "."
	}

	type writeItem struct{ src, dst string }
	writes := []writeItem{{"COJIRA-BOOTSTRAP.md", outputPath}}
	if !noExamples {
		for _, a := range bootstrapAssets[1:] {
			writes = append(writes, writeItem{a.src, filepath.Join(targetDir, a.dst)})
		}
	}

	wroteAny := false
	var results []map[string]string

	for _, w := range writes {
		content := bootstrapMD
		if w.src != "COJIRA-BOOTSTRAP.md" {
			c, readErr := readAsset(w.src)
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
			content = c
		}

		status, writeErr := writeTextFile(w.dst, content, force)
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

		if status == "written" {
			wroteAny = true
			if !jsonOut {
				fmt.Printf("[wrote] %s\n", w.dst)
			}
		} else if !jsonOut {
			fmt.Printf("[skip]  %s (already exists)\n", w.dst)
		}
		results = append(results, map[string]string{"path": w.dst, "status": status})
	}

	if jsonOut {
		env := output.BuildEnvelope(
			true, "cojira", "bootstrap",
			map[string]any{"output": outputPath},
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
