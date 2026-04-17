package meta

import (
	"fmt"
	"sort"

	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/credstore"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewAuthCmd returns the "auth" command group.
func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect and migrate cojira credential storage",
	}
	cmd.AddCommand(newAuthStatusCmd(), newAuthMigrateCmd())
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the active credential store and stored credential state",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := normalizeMetaOutputMode(cmd)
			store := credstore.ResolveStoreName()
			effective := credstore.EffectiveStoreName()
			plainExists, plainPath := credstore.HasPlainCredentials()
			keyringExists, keyringErr := credstore.KeyringStatus()
			result := map[string]any{
				"configured_store": store,
				"effective_store":  effective,
				"plain_credentials": map[string]any{
					"path":   plainPath,
					"exists": plainExists,
				},
				"keyring": map[string]any{
					"available": credstore.KeyringAvailable(),
					"exists":    keyringExists,
				},
				"loaded_keys": sortedKeys(credstore.ParseKnownProcessEnv()),
			}
			if keyringErr != nil {
				result["keyring"].(map[string]any)["error"] = keyringErr.Error()
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "cojira", "auth.status", nil, result, nil, nil, "", "", "", nil))
			}
			fmt.Printf("Credential store: %s (effective: %s)\n", store, effective)
			fmt.Printf("Plain credentials: %v (%s)\n", plainExists, plainPath)
			fmt.Printf("Keyring: available=%v stored=%v\n", credstore.KeyringAvailable(), keyringExists)
			return nil
		},
	}
	addMetaOutputFlags(cmd)
	return cmd
}

func newAuthMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Copy current credentials into a different backend",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := normalizeMetaOutputMode(cmd)
			targetStore, _ := cmd.Flags().GetString("to")
			targetStore = credstore.NormalizeStoreNamePublic(targetStore)
			if targetStore == "" || targetStore == credstore.StoreAuto {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use --to plain or --to keyring.", ExitCode: 2}
			}
			setDefault, _ := cmd.Flags().GetBool("set-default")

			values := credstore.ParseKnownProcessEnv()
			if len(values) == 0 {
				if fromStoreValues, _, err := credstore.Load(); err == nil {
					values = fromStoreValues
				}
			}
			if len(values) == 0 {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "No credentials were loaded to migrate.", ExitCode: 1}
			}

			result := map[string]any{"to": targetStore, "keys": sortedKeys(values)}
			switch targetStore {
			case credstore.StorePlain:
				path, err := credstore.WritePlainCredentials(values)
				if err != nil {
					return err
				}
				result["path"] = path
			case credstore.StoreKeyring:
				if err := credstore.SaveToKeyring(values); err != nil {
					return err
				}
				result["service"] = "cojira"
			}

			if setDefault {
				cfg, err := config.LoadWritableProjectConfig()
				if err != nil {
					return err
				}
				cfg.Data["credential_store"] = targetStore
				if err := config.WriteProjectConfig(cfg.Path, cfg.Data); err != nil {
					return err
				}
				result["config_path"] = cfg.Path
			}

			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "cojira", "auth.migrate", nil, result, nil, nil, "", "", "", nil))
			}
			fmt.Printf("Migrated credentials to %s.\n", targetStore)
			return nil
		},
	}
	cmd.Flags().String("to", "", "Target credential store: plain or keyring")
	cmd.Flags().Bool("set-default", false, "Write credential_store into the nearest .cojira.json")
	addMetaOutputFlags(cmd)
	return cmd
}

func addMetaOutputFlags(cmd *cobra.Command) {
	cmd.Flags().String("output-mode", "human", "Output mode: human, json, ndjson, summary, auto (default: human)")
}

func normalizeMetaOutputMode(cmd *cobra.Command) string {
	mode, _ := cmd.Flags().GetString("output-mode")
	if mode == "json" || mode == "ndjson" {
		output.SetMode(mode)
		return mode
	}
	output.SetMode("human")
	return "human"
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
