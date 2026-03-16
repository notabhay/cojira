package confluence

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var confluenceAPIAllowlist = []string{
	"/content",
	"/space",
	"/user",
	"/search",
}

// NewAPICmd creates the "api" subcommand.
func NewAPICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "raw <method> <path>",
		Aliases: []string{"api"},
		Short:   "Send a read-only request to the Confluence REST API",
		Long: `Send a read-only request to an allowlisted Confluence REST API resource.

Only GET is supported today.
Accepted paths must be API-relative and start with one of:
  /content
  /space
  /user
  /search`,
		Args: cobra.ExactArgs(2),
		RunE: runAPI,
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runAPI(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	method := strings.ToUpper(strings.TrimSpace(args[0]))
	pathArg := strings.TrimSpace(args[1])
	if method != "GET" {
		return apiValidationError(mode, method, pathArg, "Only GET is supported for `confluence api` right now.")
	}

	apiPath, params, err := parseAndValidateAPIPath(pathArg)
	if err != nil {
		return apiValidationError(mode, method, pathArg, err.Error())
	}

	resp, err := client.Request(method, apiPath, nil, params)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "api",
				map[string]any{"method": method, "path": pathArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching %s %s: %v\n", method, pathArg, err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := decodeAnyJSON(resp)
	if err != nil {
		return err
	}

	outputFile, _ := cmd.Flags().GetString("output")
	if outputFile != "" {
		jsonStr, err := output.JSONDumps(payload)
		if err != nil {
			return err
		}
		if err := writeFile(outputFile, jsonStr); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "api",
				map[string]any{"method": method, "path": apiPath},
				map[string]any{"saved_to": outputFile},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved %s %s to %s.\n", method, apiPath, outputFile)
			return nil
		}
		fmt.Printf("Saved API response to: %s\n", outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "api",
			map[string]any{"method": method, "path": apiPath},
			payload, nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Println(apiSummary(apiPath, payload))
		return nil
	}

	jsonStr, err := output.JSONDumps(payload)
	if err != nil {
		return err
	}
	fmt.Println(jsonStr)
	return nil
}

func parseAndValidateAPIPath(pathArg string) (string, url.Values, error) {
	parsed, err := url.Parse(pathArg)
	if err != nil {
		return "", nil, fmt.Errorf("Invalid API path: %v", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" {
		return "", nil, fmt.Errorf("Absolute URLs are not allowed. Use an API-relative path like /content/12345")
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return "", nil, fmt.Errorf("API paths must start with '/'.")
	}
	if !isAllowlistedAPIPath(parsed.Path) {
		return "", nil, fmt.Errorf("Unsupported API path. Allowed prefixes: %s", strings.Join(confluenceAPIAllowlist, ", "))
	}
	return parsed.Path, parsed.Query(), nil
}

func isAllowlistedAPIPath(path string) bool {
	for _, prefix := range confluenceAPIAllowlist {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func apiValidationError(mode string, method string, pathArg string, message string) error {
	if mode == "json" {
		errObj, _ := output.ErrorObj(cerrors.OpFailed, message, "", "", nil)
		ec := 2
		return output.PrintJSON(output.BuildEnvelope(
			false, "confluence", "api",
			map[string]any{"method": method, "path": pathArg},
			nil, nil, []any{errObj}, "", "", "", &ec,
		))
	}
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: message, ExitCode: 2}
}

func apiSummary(path string, payload any) string {
	if obj, ok := payload.(map[string]any); ok {
		if results, ok := obj["results"].([]any); ok {
			return fmt.Sprintf("Fetched %d item(s) from %s.", len(results), path)
		}
		return fmt.Sprintf("Fetched %s.", path)
	}
	if items, ok := payload.([]any); ok {
		return fmt.Sprintf("Fetched %d item(s) from %s.", len(items), path)
	}
	return fmt.Sprintf("Fetched %s.", path)
}
