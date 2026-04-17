package confluence

import (
	"fmt"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewRestrictionsCmd creates the "restrictions" subcommand.
func NewRestrictionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restrictions <page>",
		Short: "Show or update Confluence page restrictions",
		Args:  cobra.ExactArgs(1),
		RunE:  runRestrictions,
	}
	cmd.Flags().StringArray("add-read-user", nil, "User to add to read restrictions (raw=username, or username:foo / userkey:bar)")
	cmd.Flags().StringArray("remove-read-user", nil, "User to remove from read restrictions")
	cmd.Flags().StringArray("add-read-group", nil, "Group to add to read restrictions")
	cmd.Flags().StringArray("remove-read-group", nil, "Group to remove from read restrictions")
	cmd.Flags().StringArray("add-edit-user", nil, "User to add to edit restrictions")
	cmd.Flags().StringArray("remove-edit-user", nil, "User to remove from edit restrictions")
	cmd.Flags().StringArray("add-edit-group", nil, "Group to add to edit restrictions")
	cmd.Flags().StringArray("remove-edit-group", nil, "Group to remove from edit restrictions")
	cmd.Flags().Bool("dry-run", false, "Preview restriction changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runRestrictions(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "restrictions",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	expand := "restrictions.read.restrictions.user,restrictions.read.restrictions.group,restrictions.update.restrictions.user,restrictions.update.restrictions.group"
	page, err := client.GetPageByID(pageID, expand)
	if err != nil {
		return err
	}

	addReadUsers, _ := cmd.Flags().GetStringArray("add-read-user")
	removeReadUsers, _ := cmd.Flags().GetStringArray("remove-read-user")
	addReadGroups, _ := cmd.Flags().GetStringArray("add-read-group")
	removeReadGroups, _ := cmd.Flags().GetStringArray("remove-read-group")
	addEditUsers, _ := cmd.Flags().GetStringArray("add-edit-user")
	removeEditUsers, _ := cmd.Flags().GetStringArray("remove-edit-user")
	addEditGroups, _ := cmd.Flags().GetStringArray("add-edit-group")
	removeEditGroups, _ := cmd.Flags().GetStringArray("remove-edit-group")

	result := map[string]any{
		"id":           pageID,
		"title":        page["title"],
		"read_users":   restrictionNames(page, "read", "user"),
		"read_groups":  restrictionNames(page, "read", "group"),
		"edit_users":   restrictionNames(page, "update", "user"),
		"edit_groups":  restrictionNames(page, "update", "group"),
		"restrictions": page["restrictions"],
	}

	if !hasRestrictionMutations(addReadUsers, removeReadUsers, addReadGroups, removeReadGroups, addEditUsers, removeEditUsers, addEditGroups, removeEditGroups) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "restrictions",
				map[string]any{"page": pageArg, "page_id": pageID},
				result, nil, nil, "", "", "", nil,
			))
		}

		if mode == "summary" {
			fmt.Printf("Page %s has %d read and %d edit restriction entries.\n",
				pageID,
				len(result["read_users"].([]string))+len(result["read_groups"].([]string)),
				len(result["edit_users"].([]string))+len(result["edit_groups"].([]string)),
			)
			return nil
		}

		fmt.Printf("Restrictions for %v (%s)\n", result["title"], pageID)
		printRestrictionSection("Read users", result["read_users"].([]string))
		printRestrictionSection("Read groups", result["read_groups"].([]string))
		printRestrictionSection("Edit users", result["edit_users"].([]string))
		printRestrictionSection("Edit groups", result["edit_groups"].([]string))
		return nil
	}

	currentReadUsers := restrictionEntries(page, "read", "user")
	currentReadGroups := restrictionEntries(page, "read", "group")
	currentEditUsers := restrictionEntries(page, "update", "user")
	currentEditGroups := restrictionEntries(page, "update", "group")

	nextReadUsers, readUserChanges := applyUserRestrictionChanges(currentReadUsers, addReadUsers, removeReadUsers)
	nextReadGroups, readGroupChanges := applyGroupRestrictionChanges(currentReadGroups, addReadGroups, removeReadGroups)
	nextEditUsers, editUserChanges := applyUserRestrictionChanges(currentEditUsers, addEditUsers, removeEditUsers)
	nextEditGroups, editGroupChanges := applyGroupRestrictionChanges(currentEditGroups, addEditGroups, removeEditGroups)

	payload := []map[string]any{
		{
			"operation": "read",
			"restrictions": map[string]any{
				"user":  nextReadUsers,
				"group": nextReadGroups,
			},
		},
		{
			"operation": "update",
			"restrictions": map[string]any{
				"user":  nextEditUsers,
				"group": nextEditGroups,
			},
		},
	}

	changeSummary := map[string]any{
		"read_users":  readUserChanges,
		"read_groups": readGroupChanges,
		"edit_users":  editUserChanges,
		"edit_groups": editGroupChanges,
	}
	if restrictionChangeCount(changeSummary) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "restrictions", map[string]any{"page": pageArg, "page_id": pageID}, map[string]any{"changed": false, "summary": changeSummary}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("No restriction changes needed for page %s.\n", pageID)
			return nil
		}
		fmt.Printf("No restriction changes needed for page %s.\n", pageID)
		return nil
	}

	mutationResult := map[string]any{
		"id":      pageID,
		"title":   page["title"],
		"summary": changeSummary,
		"payload": payload,
	}
	target := map[string]any{"page": pageArg, "page_id": pageID}

	if dryRun {
		mutationResult["dry_run"] = true
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "restrictions", target, mutationResult, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would update restrictions on page %s.\n", pageID)
			return nil
		}
		fmt.Printf("Would update restrictions on page %s.\n", pageID)
		printRestrictionChanges(changeSummary)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "restrictions", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate restriction update for %s.\n", pageID)
		return nil
	}

	updated, err := client.UpdateRestrictions(pageID, payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.restrictions %s", pageID))
	}
	mutationResult["updated"] = true
	mutationResult["result"] = updated

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "restrictions",
			target,
			mutationResult, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Updated restrictions on page %s.\n", pageID)
		return nil
	}
	fmt.Printf("Updated restrictions on %v (%s)\n", page["title"], pageID)
	printRestrictionChanges(changeSummary)
	return nil
}

func hasRestrictionMutations(values ...[]string) bool {
	for _, group := range values {
		for _, value := range group {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

func restrictionEntries(page map[string]any, operation, subject string) []map[string]any {
	raw := getNestedSlice(page, "restrictions", operation, "restrictions", subject, "results")
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if m, ok := entry.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

func restrictionNames(page map[string]any, operation, subject string) []string {
	raw := restrictionEntries(page, operation, subject)
	items := make([]string, 0, len(raw))
	for _, m := range raw {
		switch subject {
		case "user":
			if name := restrictionUserLabel(m); name != "" {
				items = append(items, name)
			}
		case "group":
			if name := getNestedString(m, "name"); name != "" {
				items = append(items, name)
			}
		}
	}
	return items
}

func restrictionUserLabel(m map[string]any) string {
	for _, key := range []string{"displayName", "username", "userKey"} {
		if value := getNestedString(m, key); value != "" {
			return value
		}
	}
	return ""
}

func applyUserRestrictionChanges(current []map[string]any, adds, removes []string) ([]map[string]any, map[string]any) {
	out := append([]map[string]any{}, current...)
	added := []string{}
	removed := []string{}

	for _, raw := range removes {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		next := make([]map[string]any, 0, len(out))
		removedOne := false
		for _, item := range out {
			if !removedOne && userRestrictionMatches(item, ref) {
				removedOne = true
				removed = append(removed, restrictionUserLabel(item))
				continue
			}
			next = append(next, item)
		}
		out = next
	}

	for _, raw := range adds {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		exists := false
		for _, item := range out {
			if userRestrictionMatches(item, ref) {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		item := buildUserRestriction(ref)
		out = append(out, item)
		added = append(added, restrictionUserLabel(item))
	}

	return out, map[string]any{"added": added, "removed": removed}
}

func applyGroupRestrictionChanges(current []map[string]any, adds, removes []string) ([]map[string]any, map[string]any) {
	out := append([]map[string]any{}, current...)
	added := []string{}
	removed := []string{}

	for _, raw := range removes {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		next := make([]map[string]any, 0, len(out))
		removedOne := false
		for _, item := range out {
			name := getNestedString(item, "name")
			if !removedOne && strings.EqualFold(name, stripRestrictionPrefix(ref, "group")) {
				removedOne = true
				removed = append(removed, name)
				continue
			}
			next = append(next, item)
		}
		out = next
	}

	for _, raw := range adds {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		name := stripRestrictionPrefix(ref, "group")
		exists := false
		for _, item := range out {
			if strings.EqualFold(getNestedString(item, "name"), name) {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		out = append(out, map[string]any{"name": name})
		added = append(added, name)
	}

	return out, map[string]any{"added": added, "removed": removed}
}

func userRestrictionMatches(item map[string]any, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	refLower := strings.ToLower(ref)
	candidates := []string{
		strings.ToLower(getNestedString(item, "username")),
		strings.ToLower(getNestedString(item, "userKey")),
		strings.ToLower(getNestedString(item, "displayName")),
	}
	switch {
	case strings.HasPrefix(refLower, "username:"):
		want := strings.ToLower(stripRestrictionPrefix(ref, "username"))
		return candidates[0] == want
	case strings.HasPrefix(refLower, "userkey:"):
		want := strings.ToLower(stripRestrictionPrefix(ref, "userkey"))
		return candidates[1] == want
	default:
		want := strings.ToLower(ref)
		for _, candidate := range candidates {
			if candidate == want {
				return true
			}
		}
		return false
	}
}

func buildUserRestriction(ref string) map[string]any {
	ref = strings.TrimSpace(ref)
	refLower := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(refLower, "userkey:"):
		value := stripRestrictionPrefix(ref, "userkey")
		return map[string]any{"type": "known", "userKey": value, "displayName": value}
	default:
		value := stripRestrictionPrefix(ref, "username")
		return map[string]any{"type": "known", "username": value, "displayName": value}
	}
}

func stripRestrictionPrefix(ref, prefix string) string {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), prefix) {
		return strings.TrimSpace(parts[1])
	}
	if prefix == "group" {
		return strings.TrimSpace(ref)
	}
	return strings.TrimSpace(ref)
}

func restrictionChangeCount(summary map[string]any) int {
	total := 0
	for _, key := range []string{"read_users", "read_groups", "edit_users", "edit_groups"} {
		entry, _ := summary[key].(map[string]any)
		added, _ := entry["added"].([]string)
		removed, _ := entry["removed"].([]string)
		total += len(added) + len(removed)
	}
	return total
}

func printRestrictionChanges(summary map[string]any) {
	for _, key := range []struct {
		name string
		key  string
	}{
		{"Read users", "read_users"},
		{"Read groups", "read_groups"},
		{"Edit users", "edit_users"},
		{"Edit groups", "edit_groups"},
	} {
		entry, _ := summary[key.key].(map[string]any)
		added, _ := entry["added"].([]string)
		removed, _ := entry["removed"].([]string)
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		fmt.Printf("%s:\n", key.name)
		for _, value := range added {
			fmt.Printf("  + %s\n", value)
		}
		for _, value := range removed {
			fmt.Printf("  - %s\n", value)
		}
	}
}

func printRestrictionSection(label string, values []string) {
	fmt.Printf("%s: ", label)
	if len(values) == 0 {
		fmt.Println("(none)")
		return
	}
	fmt.Println()
	for _, value := range values {
		fmt.Printf("  - %s\n", value)
	}
}
