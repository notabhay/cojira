package confluence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewWatchCmd creates the "watch" command group.
func NewWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch Confluence pages or CQL results for changes (polling-first)",
	}
	cmd.AddCommand(
		newWatchPageCmd(),
		newWatchCQLCmd(),
		newWatchSpaceCmd(),
	)
	return cmd
}

func newWatchPageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "page <page>",
		Short: "Watch a Confluence page for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runWatchPage,
	}
	addConfluenceWatchFlags(cmd)
	return cmd
}

func newWatchCQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cql <query>",
		Short: "Watch a Confluence CQL result set for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runWatchCQL,
	}
	addConfluenceWatchFlags(cmd)
	cmd.Flags().Int("limit", 25, "Maximum pages to include in the CQL watch snapshot")
	return cmd
}

func newWatchSpaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "space <space-key>",
		Short: "Watch a Confluence space for changed pages",
		Args:  cobra.ExactArgs(1),
		RunE:  runWatchSpace,
	}
	addConfluenceWatchFlags(cmd)
	cmd.Flags().Int("limit", 25, "Maximum pages to include in the space watch snapshot")
	return cmd
}

func addConfluenceWatchFlags(cmd *cobra.Command) {
	cmd.Flags().Duration("interval", 30*time.Second, "Polling interval")
	cmd.Flags().Int("cycles", 0, "Number of watch cycles before exit (0 means run forever)")
	cmd.Flags().String("state-file", "", "Path to the local watch state file")
	cmd.Flags().String("on-change", "", "Shell command to run after a detected change")
	cmd.Flags().Bool("notify", false, "Send a best-effort desktop notification on change")
	cmd.Flags().String("transport", "auto", "Watch transport: auto, polling, webhook (webhook currently falls back to polling)")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
}

func runWatchPage(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	transport, err := normalizeConfluenceWatchTransport(cmd)
	if err != nil {
		return err
	}
	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		return err
	}
	scope := "page:" + pageID
	return runConfluenceWatchLoop(cmd, mode, transport, scope, func() (any, error) {
		page, err := client.GetPageByID(pageID, "version,history,body.storage")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"id":      normalizeMaybeString(page["id"]),
			"title":   normalizeMaybeString(page["title"]),
			"version": intFromAny(getNested(page, "version", "number"), 0),
			"updated": getNestedString(page, "history", "lastUpdated", "when"),
			"body":    getNestedString(page, "body", "storage", "value"),
		}, nil
	})
}

func runWatchCQL(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	transport, err := normalizeConfluenceWatchTransport(cmd)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(args[0])
	limit, _ := cmd.Flags().GetInt("limit")
	scope := "cql:" + query
	return runConfluenceWatchLoop(cmd, mode, transport, scope, func() (any, error) {
		data, err := client.CQL(query, limit, 0)
		if err != nil {
			return nil, err
		}
		items := extractResults(data)
		return map[string]any{
			"query":   query,
			"results": summarizeWatchedPages(items),
			"count":   len(items),
		}, nil
	})
}

func runWatchSpace(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	transport, err := normalizeConfluenceWatchTransport(cmd)
	if err != nil {
		return err
	}
	spaceKey := strings.TrimSpace(args[0])
	limit, _ := cmd.Flags().GetInt("limit")
	query := fmt.Sprintf(`space="%s" order by lastmodified desc`, strings.ReplaceAll(spaceKey, `"`, `\"`))
	scope := "space:" + spaceKey
	return runConfluenceWatchLoop(cmd, mode, transport, scope, func() (any, error) {
		data, err := client.CQL(query, limit, 0)
		if err != nil {
			return nil, err
		}
		items := extractResults(data)
		return map[string]any{
			"space":   spaceKey,
			"results": summarizeWatchedPages(items),
			"count":   len(items),
		}, nil
	})
}

func normalizeConfluenceWatchTransport(cmd *cobra.Command) (string, error) {
	transport, _ := cmd.Flags().GetString("transport")
	transport = strings.ToLower(strings.TrimSpace(transport))
	switch transport {
	case "", "auto", "polling":
		return "polling", nil
	case "webhook":
		output.EmitEvent("warning", map[string]any{
			"message": "webhook transport is not yet implemented; falling back to polling",
		})
		return "polling", nil
	default:
		return "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported transport %q. Use auto, polling, or webhook.", transport),
			ExitCode: 2,
		}
	}
}

func runConfluenceWatchLoop(cmd *cobra.Command, mode, transport, scope string, fetch func() (any, error)) error {
	interval, _ := cmd.Flags().GetDuration("interval")
	cycles, _ := cmd.Flags().GetInt("cycles")
	stateFile, _ := cmd.Flags().GetString("state-file")
	onChange, _ := cmd.Flags().GetString("on-change")
	notify, _ := cmd.Flags().GetBool("notify")
	if strings.TrimSpace(stateFile) == "" {
		stateFile = confluencePollStatePath("watch:" + scope)
	}

	type pollSnapshot struct {
		Hash      string `json:"hash"`
		UpdatedAt string `json:"updated_at"`
	}

	runOnce := func(iteration int) (bool, any, error) {
		payload, err := fetch()
		if err != nil {
			return false, nil, err
		}
		hash, err := confluenceHashValue(payload)
		if err != nil {
			return false, nil, err
		}
		prev, _ := confluenceReadPollSnapshot(stateFile)
		changed := prev.Hash != "" && prev.Hash != hash
		if err := confluenceWritePollSnapshot(stateFile, pollSnapshot{Hash: hash, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			return false, nil, err
		}
		eventPayload := map[string]any{
			"scope":      scope,
			"transport":  transport,
			"changed":    changed,
			"iteration":  iteration,
			"state_file": stateFile,
		}
		if changed {
			output.EmitEvent("watch.change", eventPayload)
			if notify {
				confluenceNotify("cojira detected a Confluence change", scope)
			}
			if strings.TrimSpace(onChange) != "" {
				_ = runConfluenceShellHook(cmd, onChange)
			}
		} else {
			output.EmitEvent("watch.tick", eventPayload)
		}
		return changed, payload, nil
	}

	iterations := 0
	for {
		iterations++
		changed, payload, err := runOnce(iterations)
		if err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "watch", map[string]any{"scope": scope}, map[string]any{
				"scope":      scope,
				"state_file": stateFile,
				"changed":    changed,
				"payload":    payload,
				"iterations": iterations,
				"transport":  transport,
			}, nil, nil, "", "", "", nil))
		}
		if mode == "ndjson" {
			if err := output.PrintJSON(map[string]any{
				"type":       "watch",
				"tool":       "confluence",
				"scope":      scope,
				"state_file": stateFile,
				"changed":    changed,
				"payload":    payload,
				"iteration":  iterations,
				"transport":  transport,
			}); err != nil {
				return err
			}
		} else if changed {
			fmt.Printf("Change detected for %s.\n", scope)
		} else if mode != "summary" {
			fmt.Printf("No change for %s.\n", scope)
		}
		if cycles > 0 && iterations >= cycles {
			break
		}
		time.Sleep(interval)
	}
	if mode == "summary" {
		fmt.Printf("Completed %d watch cycle(s) for %s using %s.\n", iterations, scope, transport)
	}
	return nil
}

func summarizeWatchedPages(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":      normalizeMaybeString(item["id"]),
			"title":   normalizeMaybeString(item["title"]),
			"type":    normalizeMaybeString(item["type"]),
			"excerpt": normalizeMaybeString(item["excerpt"]),
			"url":     getNestedString(item, "_links", "webui"),
		})
	}
	return out
}

func confluenceHashValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func confluenceReadPollSnapshot(path string) (struct {
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updated_at"`
}, error) {
	var snapshot struct {
		Hash      string `json:"hash"`
		UpdatedAt string `json:"updated_at"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	err = json.Unmarshal(data, &snapshot)
	return snapshot, err
}

func confluenceWritePollSnapshot(path string, snapshot any) error {
	if err := os.MkdirAll(confluenceFilepathDir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func confluenceFilepathDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "/"
	}
	return path[:idx]
}

func confluenceNotify(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title %q`, body, title)).Run()
	default:
		if _, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command("notify-send", title, body).Run()
		}
	}
}

func runConfluenceShellHook(cmd *cobra.Command, script string) error {
	child := exec.CommandContext(cmd.Context(), "/bin/sh", "-lc", script)
	child.Stdout = cmd.OutOrStdout()
	child.Stderr = cmd.ErrOrStderr()
	return child.Run()
}

func getNested(m map[string]any, keys ...string) any {
	var cur any = m
	for _, key := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[key]
	}
	return cur
}
