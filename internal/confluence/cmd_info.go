package confluence

import (
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewInfoCmd creates the "info" subcommand.
func NewInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <page>",
		Short: "Show page metadata",
		Args:  cobra.ExactArgs(1),
		RunE:  runInfo,
	}
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runInfo(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "info",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	page, err := client.GetPageByID(pageID, "version,history,space,ancestors,children.page")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "info",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching page %s: %v\n", pageID, err)
		return err
	}

	ancestors, _ := page["ancestors"].([]any)
	var parentID, parentTitle any
	if len(ancestors) > 0 {
		if parent, ok := ancestors[len(ancestors)-1].(map[string]any); ok {
			parentID = parent["id"]
			parentTitle = parent["title"]
		}
	}

	childrenResults := getNestedSlice(page, "children", "page", "results")
	spaceKey := getNestedString(page, "space", "key")
	versionNum := getNestedFloat(page, "version", "number")
	lastModified := getNestedString(page, "version", "when")
	lastModifiedBy := getNestedString(page, "version", "by", "displayName")
	createdDate := getNestedString(page, "history", "createdDate")
	createdBy := getNestedString(page, "history", "createdBy", "displayName")

	info := map[string]any{
		"id":               page["id"],
		"title":            page["title"],
		"space":            spaceKey,
		"version":          int(versionNum),
		"last_modified":    lastModified,
		"last_modified_by": lastModifiedBy,
		"created_date":     createdDate,
		"created_by":       createdBy,
		"parent_id":        parentID,
		"parent_title":     parentTitle,
		"children_count":   len(childrenResults),
		"url":              fmt.Sprintf("%s/pages/viewpage.action?pageId=%v", client.BaseURL(), page["id"]),
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "info",
			map[string]any{"page": pageArg, "page_id": pageID},
			info, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		summary := fmt.Sprintf("%v: %v (Space: %s, Version: %d", info["id"], info["title"], spaceKey, int(versionNum))
		if lastModified != "" {
			summary += ", Updated: " + lastModified
			if lastModifiedBy != "" {
				summary += " by " + lastModifiedBy
			}
		}
		summary += ")"
		fmt.Println(summary)
		return nil
	}

	fmt.Printf("ID:       %v\n", info["id"])
	fmt.Printf("Title:    %v\n", info["title"])
	fmt.Printf("Space:    %s\n", spaceKey)
	fmt.Printf("Version:  %d\n", int(versionNum))
	if createdDate != "" || createdBy != "" {
		fmt.Printf("Created:  %s", stringOr(createdDate, "-"))
		if createdBy != "" {
			fmt.Printf(" by %s", createdBy)
		}
		fmt.Println()
	}
	if lastModified != "" || lastModifiedBy != "" {
		fmt.Printf("Updated:  %s", stringOr(lastModified, "-"))
		if lastModifiedBy != "" {
			fmt.Printf(" by %s", lastModifiedBy)
		}
		fmt.Println()
	}
	if parentID != nil {
		fmt.Printf("Parent:   %v (%v)\n", parentID, parentTitle)
	} else {
		fmt.Printf("Parent:   (root)\n")
	}
	fmt.Printf("Children: %d\n", len(childrenResults))
	fmt.Printf("URL:      %v\n", info["url"])
	return nil
}

// getNestedString extracts a string from nested maps.
func getNestedString(m map[string]any, keys ...string) string {
	var cur any = m
	for _, key := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[key]
	}
	s, _ := cur.(string)
	return s
}

// getNestedFloat extracts a float64 from nested maps.
func getNestedFloat(m map[string]any, keys ...string) float64 {
	var cur any = m
	for _, key := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return 0
		}
		cur = mm[key]
	}
	f, _ := cur.(float64)
	return f
}

// getNestedSlice extracts a slice from nested maps.
func getNestedSlice(m map[string]any, keys ...string) []any {
	var cur any = m
	for _, key := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[key]
	}
	s, _ := cur.([]any)
	return s
}

func stringOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
