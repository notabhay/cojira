package confluence

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewBlogCmd creates the "blog" command group.
func NewBlogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blog",
		Short: "Manage Confluence blog posts",
	}
	cmd.AddCommand(newBlogListCmd())
	cmd.AddCommand(newBlogCreateCmd())
	cmd.AddCommand(newBlogUpdateCmd())
	cmd.AddCommand(newBlogDeleteCmd())
	return cmd
}

func newBlogListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [query]",
		Short: "List blog posts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}

			cfgData := loadProjectConfigData()
			space, _ := cmd.Flags().GetString("space")
			if space == "" {
				space = defaultSpace(cfgData)
			}
			all, _ := cmd.Flags().GetBool("all")
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			query := ""
			if len(args) > 0 {
				query = strings.TrimSpace(args[0])
			}

			items := make([]map[string]any, 0)
			total := 0
			if query != "" {
				cql := fmt.Sprintf(`type=blogpost AND title~"%s"`, escapeCQLString(query))
				if space != "" {
					cql = fmt.Sprintf(`space="%s" AND %s`, escapeCQLString(space), cql)
				}
				if all {
					if pageSize <= 0 {
						pageSize = 50
					}
					offset := start
					for {
						data, err := client.CQL(cql, pageSize, offset)
						if err != nil {
							return err
						}
						pageItems := extractResults(data)
						total = intFromAny(data["size"], total)
						items = append(items, pageItems...)
						offset += len(pageItems)
						if len(pageItems) == 0 {
							break
						}
					}
				} else {
					data, err := client.CQL(cql, limit, start)
					if err != nil {
						return err
					}
					items = extractResults(data)
					total = intFromAny(data["size"], len(items))
				}
			} else {
				if all {
					if pageSize <= 0 {
						pageSize = 50
					}
					offset := start
					for {
						data, err := client.ListBlogPosts(space, pageSize, offset)
						if err != nil {
							return err
						}
						pageItems := extractResults(data)
						total = intFromAny(data["size"], total)
						items = append(items, pageItems...)
						offset += len(pageItems)
						if len(pageItems) == 0 {
							break
						}
					}
				} else {
					data, err := client.ListBlogPosts(space, limit, start)
					if err != nil {
						return err
					}
					items = extractResults(data)
					total = intFromAny(data["size"], len(items))
				}
			}

			target := map[string]any{}
			if space != "" {
				target["space"] = space
			}
			if query != "" {
				target["query"] = query
			}

			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.list", target, map[string]any{"posts": items, "summary": map[string]any{"count": len(items), "total": total}}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d blog post(s).\n", len(items))
				return nil
			}
			if len(items) == 0 {
				fmt.Println("No blog posts found.")
				return nil
			}
			fmt.Printf("Blog posts (%d):\n\n", len(items))
			for _, item := range items {
				fmt.Printf("  %-12v %-10s %v\n", item["id"], getNestedString(item, "space", "key"), item["title"])
			}
			return nil
		},
	}
	cmd.Flags().String("space", "", "Space key (defaults to confluence.default_space)")
	cmd.Flags().Bool("all", false, "Fetch all blog posts")
	cmd.Flags().Int("limit", 20, "Maximum blog posts to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newBlogCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a blog post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}

			cfgData := loadProjectConfigData()
			title := strings.TrimSpace(args[0])
			space, _ := cmd.Flags().GetString("space")
			if space == "" {
				space = defaultSpace(cfgData)
			}
			filePath, _ := cmd.Flags().GetString("file")
			format, _ := cmd.Flags().GetString("format")
			planMode, _ := cmd.Flags().GetBool("plan")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")

			if title == "" {
				return &cerrors.CojiraError{Code: cerrors.InvalidTitle, Message: "Title is required.", ExitCode: 1}
			}
			if space == "" {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Space key is required.", ExitCode: 2}
			}

			body := ""
			if filePath != "" {
				body, err = readTextFile(filePath)
				if err != nil {
					return err
				}
			}
			body, err = convertStorageBody(body, format)
			if err != nil {
				return err
			}

			target := map[string]any{"title": title, "space": space}
			if planMode {
				result := map[string]any{"plan": true, "title": title, "space": space}
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.create", target, result, nil, nil, "", "", "", nil))
				}
				if mode == "summary" {
					fmt.Printf("Would create blog post %q in %s.\n", title, space)
					return nil
				}
				fmt.Printf("Would create blog post %q in %s.\n", title, space)
				return nil
			}

			if idemKey != "" && idempotency.IsDuplicate(idemKey) {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.create", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
				}
				fmt.Printf("Skipped duplicate blog create in %s.\n", space)
				return nil
			}

			payload := map[string]any{
				"type":  "blogpost",
				"title": title,
				"space": map[string]any{"key": space},
				"body": map[string]any{
					"storage": map[string]any{
						"value":          body,
						"representation": "storage",
					},
				},
			}
			result, err := client.CreatePage(payload)
			if err != nil {
				return err
			}
			if idemKey != "" {
				_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.blog.create %s", space))
			}
			postID := fmt.Sprintf("%v", result["id"])
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.create", target, map[string]any{"id": postID, "title": title, "space": space}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Created blog post %s: %s.\n", postID, title)
				return nil
			}
			fmt.Printf("Created blog post %s: %s\n", postID, title)
			return nil
		},
	}
	cmd.Flags().String("space", "", "Space key (defaults to confluence.default_space)")
	cmd.Flags().StringP("file", "f", "", "Content file (storage-format XHTML)")
	cmd.Flags().String("format", "storage", "Body format: storage or markdown")
	cmd.Flags().Bool("plan", false, "Preview create without applying")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newBlogUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <blog> <file>",
		Short: "Update a blog post from a file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}

			blogArg := args[0]
			filePath := args[1]
			titleFlag, _ := cmd.Flags().GetString("title")
			format, _ := cmd.Flags().GetString("format")
			minorEdit, _ := cmd.Flags().GetBool("minor")
			diffMode, _ := cmd.Flags().GetBool("diff")
			previewMode, _ := cmd.Flags().GetBool("preview")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")

			content, err := readTextFile(filePath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return &cerrors.CojiraError{Code: cerrors.EmptyContent, Message: "Refusing to update with empty content.", ExitCode: 1}
			}
			content, err = convertStorageBody(content, format)
			if err != nil {
				return err
			}

			page, err := client.GetPageByID(blogArg, "version,body.storage")
			if err != nil {
				return err
			}
			pageID := fmt.Sprintf("%v", page["id"])
			title := titleFlag
			if title == "" {
				title, _ = page["title"].(string)
			}
			oldVersion := int(getNestedFloat(page, "version", "number"))

			if diffMode || previewMode {
				current := getNestedString(page, "body", "storage", "value")
				diffText, additions, deletions := computeUnifiedDiff(current, content, pageID)
				changed := diffText != "" || title != getNestedString(page, "title")
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.update", map[string]any{"blog": blogArg, "blog_id": pageID}, map[string]any{"changed": changed, "diff": diffText, "summary": map[string]any{"additions": additions, "deletions": deletions}}, nil, nil, "", "", "", nil))
				}
				if mode == "summary" {
					status := "changes detected"
					if !changed {
						status = "no changes"
					}
					fmt.Printf("Previewed update for blog post %s (%s).\n", pageID, status)
					return nil
				}
				if diffText == "" {
					fmt.Println("No content changes.")
				} else {
					fmt.Print(diffText)
					fmt.Printf("\n%d addition(s), %d deletion(s)\n", additions, deletions)
				}
				return nil
			}

			if idemKey != "" && idempotency.IsDuplicate(idemKey) {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.update", map[string]any{"blog": blogArg, "blog_id": pageID}, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
				}
				fmt.Printf("Skipped duplicate blog update for %s.\n", pageID)
				return nil
			}

			payload := map[string]any{
				"type":  "blogpost",
				"title": title,
				"version": map[string]any{
					"number":    oldVersion + 1,
					"minorEdit": minorEdit,
				},
				"body": map[string]any{
					"storage": map[string]any{
						"value":          content,
						"representation": "storage",
					},
				},
			}
			_, err = client.UpdatePage(pageID, payload)
			if err != nil {
				return err
			}
			if idemKey != "" {
				_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.blog.update %s", pageID))
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.update", map[string]any{"blog": blogArg, "blog_id": pageID}, map[string]any{"id": pageID, "title": title, "version_from": oldVersion, "version_to": oldVersion + 1}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Updated blog post %s (%s).\n", pageID, title)
				return nil
			}
			fmt.Printf("Updated blog post %s (%s)\n", pageID, title)
			return nil
		},
	}
	cmd.Flags().String("title", "", "New title (optional)")
	cmd.Flags().String("format", "storage", "Body format: storage or markdown")
	cmd.Flags().Bool("minor", false, "Mark as minor edit")
	cmd.Flags().Bool("diff", false, "Show a unified diff and exit without updating")
	cmd.Flags().Bool("preview", false, "Alias for --diff")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newBlogDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <blog>",
		Short: "Delete a blog post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			blogID := args[0]
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			yes, _ := cmd.Flags().GetBool("yes")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			target := map[string]any{"blog": blogID}
			if dryRun {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.delete", target, map[string]any{"dry_run": true}, nil, nil, "", "", "", nil))
				}
				if mode == "summary" {
					fmt.Printf("Would delete blog post %s.\n", blogID)
					return nil
				}
				fmt.Printf("Would delete blog post %s.\n", blogID)
				return nil
			}
			if !yes {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Deletion is destructive. Preview with --dry-run first, then rerun with --yes.", ExitCode: 2}
			}
			if idemKey != "" && idempotency.IsDuplicate(idemKey) {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
				}
				fmt.Printf("Skipped duplicate blog delete for %s.\n", blogID)
				return nil
			}
			if err := client.DeleteContent(blogID); err != nil {
				return err
			}
			if idemKey != "" {
				_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.blog.delete %s", blogID))
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "blog.delete", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Deleted blog post %s.\n", blogID)
				return nil
			}
			fmt.Printf("Deleted blog post %s.\n", blogID)
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "Preview deletion without applying")
	cmd.Flags().Bool("yes", false, "Confirm destructive deletion")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}
