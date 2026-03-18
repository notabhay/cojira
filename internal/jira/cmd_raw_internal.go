package jira

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewRawInternalCmd creates the experimental internal-API passthrough subcommand.
func NewRawInternalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "raw-internal <service> <method> <path-or-url>",
		Short: "EXPERIMENTAL: Send an authenticated request to Jira internal APIs",
		Long: "EXPERIMENTAL: Send an authenticated request to Jira internal APIs.\n" +
			"Services: rest, agile, greenhopper, dev-status, absolute.\n" +
			"Use service=absolute only for same-host URLs on the configured Jira instance.",
		Args: cobra.ExactArgs(3),
		RunE: runRawInternal,
	}
	cmd.Flags().String("body", "", "Request body file for POST/PUT")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.Flags().String("api-base", "auto", "For service=dev-status: auto, latest, or 1.0")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runRawInternal(cmd *cobra.Command, args []string) error {
	if err := requireExperimentalJira(cmd); err != nil {
		return err
	}
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	service := strings.TrimSpace(strings.ToLower(args[0]))
	method := strings.ToUpper(strings.TrimSpace(args[1]))
	pathArg := strings.TrimSpace(args[2])
	bodyFile, _ := cmd.Flags().GetString("body")
	outputFile, _ := cmd.Flags().GetString("output")
	apiBaseFlag, _ := cmd.Flags().GetString("api-base")

	if !allowedRawMethod(method) {
		return rawValidationError(mode, method, pathArg, "Unsupported method. Use GET, POST, PUT, or DELETE.")
	}

	requestURL, params, err := resolveInternalRequestURL(client, service, pathArg, apiBaseFlag)
	if err != nil {
		return rawValidationError(mode, method, pathArg, err.Error())
	}

	var body []byte
	if bodyFile != "" {
		text, err := readTextFile(bodyFile)
		if err != nil {
			return err
		}
		body = []byte(text)
	}

	resp, err := client.RequestURL(method, requestURL, body, params)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "raw-internal",
				map[string]any{"service": service, "method": method, "path": pathArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, hasBody, err := decodeOptionalRawResponse(resp)
	if err != nil {
		return err
	}
	result := payload
	if !hasBody {
		result = map[string]any{"no_content": true}
	}

	if outputFile != "" {
		jsonStr, err := output.JSONDumps(result)
		if err != nil {
			return err
		}
		if err := writeFile(outputFile, jsonStr); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "raw-internal",
				map[string]any{"service": service, "method": method, "path": pathArg},
				map[string]any{"saved_to": outputFile},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved %s %s %s to %s.\n", service, method, pathArg, outputFile)
			return nil
		}
		fmt.Printf("Saved raw internal Jira response to: %s\n", outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "raw-internal",
			map[string]any{"service": service, "method": method, "path": pathArg},
			map[string]any{"url": requestURL, "response": result},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Println(rawSummary(method, requestURL, result))
		return nil
	}
	jsonStr, err := output.JSONDumps(result)
	if err != nil {
		return err
	}
	fmt.Println(jsonStr)
	return nil
}

func resolveInternalRequestURL(client *Client, service string, pathArg string, apiBaseFlag string) (string, url.Values, error) {
	apiBaseFlag = strings.TrimSpace(strings.ToLower(apiBaseFlag))
	switch service {
	case "rest":
		return resolveRelativeInternalURL(client.RestBaseURL(), pathArg)
	case "agile":
		return resolveRelativeInternalURL(client.AgileBaseURL(), pathArg)
	case "greenhopper":
		return resolveRelativeInternalURL(client.GreenhopperBaseURL(), pathArg)
	case "dev-status":
		var base string
		switch apiBaseFlag {
		case "", "auto":
			base = developmentBaseCandidates(client)[0]
		case "latest":
			base = client.BaseURL() + "/rest/dev-status/latest"
		case "1.0":
			base = client.BaseURL() + "/rest/dev-status/1.0"
		default:
			return "", nil, fmt.Errorf("Unsupported --api-base %q for dev-status. Use auto, latest, or 1.0", apiBaseFlag)
		}
		return resolveRelativeInternalURL(base, pathArg)
	case "absolute":
		parsed, err := url.Parse(pathArg)
		if err != nil {
			return "", nil, fmt.Errorf("Invalid absolute URL: %v", err)
		}
		if !parsed.IsAbs() || parsed.Host == "" {
			return "", nil, fmt.Errorf("Service=absolute requires a full absolute URL")
		}
		baseParsed, err := url.Parse(client.BaseURL())
		if err != nil {
			return "", nil, err
		}
		if !strings.EqualFold(parsed.Host, baseParsed.Host) {
			return "", nil, fmt.Errorf("Absolute internal requests must stay on the configured Jira host")
		}
		return parsed.String(), nil, nil
	default:
		return "", nil, fmt.Errorf("Unsupported internal Jira service %q. Use rest, agile, greenhopper, dev-status, or absolute", service)
	}
}

func resolveRelativeInternalURL(base string, pathArg string) (string, url.Values, error) {
	parsed, err := url.Parse(pathArg)
	if err != nil {
		return "", nil, fmt.Errorf("Invalid API path: %v", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" {
		return "", nil, fmt.Errorf("Use an API-relative path like /issue/summary or service=absolute for full URLs")
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return "", nil, fmt.Errorf("API paths must start with '/'")
	}
	return strings.TrimRight(base, "/") + parsed.Path, parsed.Query(), nil
}
