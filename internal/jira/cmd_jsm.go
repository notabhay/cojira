package jira

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewJSMCmd creates the "jsm" command group.
func NewJSMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jsm",
		Aliases: []string{"sm"},
		Short:   "Jira Service Management requests, queues, approvals, and SLAs",
	}
	cmd.AddCommand(
		newJSMDeskCmd(),
		newJSMQueueCmd(),
		newJSMRequestCmd(),
		newJSMApprovalCmd(),
		newJSMSLACmd(),
	)
	return cmd
}

func newJSMDeskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "desk", Short: "List service desks"}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List visible service desks",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			result, err := client.ListServiceDesks(limit, start)
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.desk.list", nil, result, "Listed service desks.")
		},
	}
	listCmd.Flags().Int("limit", 25, "Maximum desks to fetch")
	listCmd.Flags().Int("start", 0, "Start offset")
	cli.AddOutputFlags(listCmd, true)
	cmd.AddCommand(listCmd)
	return cmd
}

func newJSMQueueCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "queue", Short: "Inspect service desk queues"}
	listCmd := &cobra.Command{
		Use:   "list <service-desk-id>",
		Short: "List queues for a service desk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			result, err := client.ListServiceDeskQueues(args[0], limit, start)
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.queue.list", map[string]any{"service_desk_id": args[0]}, result, "Listed queues.")
		},
	}
	listCmd.Flags().Int("limit", 25, "Maximum queues to fetch")
	listCmd.Flags().Int("start", 0, "Start offset")
	cli.AddOutputFlags(listCmd, true)

	issuesCmd := &cobra.Command{
		Use:   "issues <service-desk-id> <queue-id>",
		Short: "List issues in a service desk queue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			result, err := client.ListQueueIssues(args[0], args[1], limit, start)
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.queue.issues", map[string]any{"service_desk_id": args[0], "queue_id": args[1]}, result, "Listed queue issues.")
		},
	}
	issuesCmd.Flags().Int("limit", 25, "Maximum queue issues to fetch")
	issuesCmd.Flags().Int("start", 0, "Start offset")
	cli.AddOutputFlags(issuesCmd, true)

	cmd.AddCommand(listCmd, issuesCmd)
	return cmd
}

func newJSMRequestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "request", Short: "Inspect customer requests"}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List visible customer requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			result, err := client.ListCustomerRequests(limit, start)
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.request.list", nil, result, "Listed customer requests.")
		},
	}
	listCmd.Flags().Int("limit", 25, "Maximum requests to fetch")
	listCmd.Flags().Int("start", 0, "Start offset")
	cli.AddOutputFlags(listCmd, true)

	getCmd := &cobra.Command{
		Use:   "get <request>",
		Short: "Get a customer request by id or issue key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			result, err := client.GetCustomerRequest(args[0])
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.request.get", map[string]any{"request": args[0]}, result, "Fetched customer request.")
		},
	}
	cli.AddOutputFlags(getCmd, true)
	cmd.AddCommand(listCmd, getCmd)
	return cmd
}

func newJSMApprovalCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "approval", Short: "Inspect or action approvals"}
	listCmd := &cobra.Command{
		Use:   "list <request>",
		Short: "List approvals on a request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			result, err := client.ListRequestApprovals(args[0])
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.approval.list", map[string]any{"request": args[0]}, result, "Listed approvals.")
		},
	}
	cli.AddOutputFlags(listCmd, true)

	approveCmd := &cobra.Command{
		Use:   "approve <request> <approval-id>",
		Short: "Approve a request approval",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			result, err := client.DecideApproval(args[0], args[1], "approve")
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.approval.approve", map[string]any{"request": args[0], "approval_id": args[1]}, result, "Approved request.")
		},
	}
	cli.AddOutputFlags(approveCmd, true)

	declineCmd := &cobra.Command{
		Use:   "decline <request> <approval-id>",
		Short: "Decline a request approval",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			result, err := client.DecideApproval(args[0], args[1], "decline")
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.approval.decline", map[string]any{"request": args[0], "approval_id": args[1]}, result, "Declined request.")
		},
	}
	cli.AddOutputFlags(declineCmd, true)

	cmd.AddCommand(listCmd, approveCmd, declineCmd)
	return cmd
}

func newJSMSLACmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sla", Short: "Inspect request SLA state"}
	listCmd := &cobra.Command{
		Use:   "list <request>",
		Short: "List SLA state for a request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			result, err := client.ListRequestSLAs(args[0])
			if err != nil {
				return err
			}
			return printJSMResult(mode, "jsm.sla.list", map[string]any{"request": args[0]}, result, "Listed request SLA state.")
		},
	}
	cli.AddOutputFlags(listCmd, true)
	cmd.AddCommand(listCmd)
	return cmd
}

func printJSMResult(mode, command string, target map[string]any, result map[string]any, summary string) error {
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", command, target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Println(summary)
		return nil
	}
	fmt.Println(summary)
	return output.PrintJSON(result)
}
