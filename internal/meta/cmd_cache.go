package meta

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// NewCacheCmd returns the "cache" command group.
func NewCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and clear the shared HTTP cache",
	}
	cmd.AddCommand(
		newCacheInspectCmd(),
		newCacheStatsCmd(),
		newCacheClearCmd(),
	)
	return cmd
}

func newCacheInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "List cache entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			varyKey, _ := cmd.Flags().GetString("vary-key")
			cfg := httpclient.DefaultCacheConfig()
			entries, err := httpclient.InspectCache(cfg, varyKey)
			if err != nil {
				return err
			}
			result := map[string]any{"dir": cfg.Dir, "entries": entries}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "cojira", "cache.inspect", map[string]any{"vary_key": varyKey}, result, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d cache %s in %s.\n", len(entries), pluralize(len(entries), "entry", "entries"), cfg.Dir)
				return nil
			}
			if len(entries) == 0 {
				fmt.Printf("No cache entries in %s.\n", cfg.Dir)
				return nil
			}
			rows := make([][]string, 0, len(entries))
			for _, entry := range entries {
				rows = append(rows, []string{
					output.Truncate(entry.Method, 8),
					output.Truncate(fmt.Sprintf("%d", entry.StatusCode), 6),
					output.Truncate(entry.VaryKey, 18),
					output.Truncate(entry.RequestURL, 72),
				})
			}
			fmt.Printf("Cache entries in %s:\n\n", cfg.Dir)
			fmt.Println(output.TableString([]string{"METHOD", "STATUS", "VARY KEY", "URL"}, rows))
			return nil
		},
	}
	cmd.Flags().String("vary-key", "", "Only inspect entries for a specific vary key")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newCacheStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show cache statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			varyKey, _ := cmd.Flags().GetString("vary-key")
			cfg := httpclient.DefaultCacheConfig()
			stats, err := httpclient.CacheStatistics(cfg, varyKey)
			if err != nil {
				return err
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "cojira", "cache.stats", map[string]any{"vary_key": varyKey}, stats, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Cache has %d %s using %d bytes.\n", stats.Entries, pluralize(stats.Entries, "entry", "entries"), stats.Bytes)
				return nil
			}
			fmt.Printf("Cache directory: %s\n", stats.Dir)
			fmt.Printf("Entries: %d\n", stats.Entries)
			fmt.Printf("Bytes: %d\n", stats.Bytes)
			fmt.Printf("Unique vary keys: %d\n", stats.UniqueVaryKey)
			if stats.NewestUnix > 0 {
				fmt.Printf("Newest: %d\n", stats.NewestUnix)
			}
			if stats.OldestUnix > 0 {
				fmt.Printf("Oldest: %d\n", stats.OldestUnix)
			}
			return nil
		},
	}
	cmd.Flags().String("vary-key", "", "Only report stats for a specific vary key")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newCacheClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear cache entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			varyKey, _ := cmd.Flags().GetString("vary-key")
			yes, _ := cmd.Flags().GetBool("yes")
			if varyKey == "" && !yes {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Refusing to clear the entire cache without --yes.",
					ExitCode: 2,
				}
			}
			cfg := httpclient.DefaultCacheConfig()
			removed, err := httpclient.ClearCache(cfg, varyKey)
			if err != nil {
				return err
			}
			result := map[string]any{"dir": cfg.Dir, "removed": removed, "vary_key": varyKey}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "cojira", "cache.clear", map[string]any{"vary_key": varyKey}, result, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Removed %d cache %s.\n", removed, pluralize(removed, "entry", "entries"))
				return nil
			}
			fmt.Printf("Removed %d cache %s from %s.\n", removed, pluralize(removed, "entry", "entries"), cfg.Dir)
			return nil
		},
	}
	cmd.Flags().String("vary-key", "", "Only clear entries for a specific vary key")
	cmd.Flags().Bool("yes", false, "Confirm clearing the entire cache")
	cli.AddOutputFlags(cmd, true)
	return cmd
}
