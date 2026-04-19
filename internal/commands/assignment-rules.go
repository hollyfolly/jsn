package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jacebenson/jsn/internal/appctx"
	"github.com/jacebenson/jsn/internal/config"
	"github.com/jacebenson/jsn/internal/output"
	"github.com/jacebenson/jsn/internal/sdk"
	"github.com/jacebenson/jsn/internal/tui"
	"github.com/spf13/cobra"
)

// assignmentRulesFlags holds the flags for the assignment-rules command.
type assignmentRulesFlags struct {
	limit  int
	table  string
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
}

// NewAssignmentRulesCmd creates the assignment-rules command.
func NewAssignmentRulesCmd() *cobra.Command {
	var flags assignmentRulesFlags

	cmd := &cobra.Command{
		Use:   "assignment-rules [<name_or_sys_id>]",
		Short: "Manage assignment rules (sysrule_assignment)",
		Long: `List and inspect ServiceNow assignment rules.

Assignment rules automatically assign records to users or groups based on conditions.

Usage:
  jsn assignment-rules <name_or_sys_id>              Show rule details
  jsn assignment-rules --search <term>               Fuzzy search on name
  jsn assignment-rules --table <name>                Filter by table
  jsn assignment-rules --active                      Show only active rules

Examples:
  jsn assignment-rules "Assign to IT"
  jsn assignment-rules --table incident --active`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runAssignmentRulesShow(cmd, args[0])
			}
			return runAssignmentRulesList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of rules to fetch")
	cmd.Flags().StringVarP(&flags.table, "table", "t", "", "Filter by table name")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active rules")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all rules (no limit)")

	return cmd
}

// runAssignmentRulesList executes the assignment-rules list command.
func runAssignmentRulesList(cmd *cobra.Command, flags assignmentRulesFlags) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	outputWriter := appCtx.Output.(*output.Writer)
	cfg := appCtx.Config.(*config.Config)
	profile := cfg.GetActiveProfile()
	instanceURL := ""
	if profile != nil {
		instanceURL = profile.InstanceURL
	}

	sdkClient := appCtx.SDK.(*sdk.Client)

	// Build query
	var queryParts []string
	if flags.active {
		queryParts = append(queryParts, "active=true")
	}
	if flags.table != "" {
		queryParts = append(queryParts, fmt.Sprintf("table=%s", flags.table))
	}
	if flags.search != "" {
		queryParts = append(queryParts, fmt.Sprintf("nameLIKE%s", flags.search))
	}
	if flags.query != "" {
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sysrule_assignment"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode - use paginated picker
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		selected, err := pickAssignmentRulePaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selected == "" {
			return fmt.Errorf("no rule selected")
		}
		return runAssignmentRulesShow(cmd, selected)
	}

	// Non-interactive: fetch and display
	limit := flags.limit
	if flags.all {
		limit = 0
	}

	opts := &sdk.ListRecordsOptions{
		Limit:     limit,
		Query:     sysparmQuery,
		OrderBy:   flags.order,
		OrderDesc: flags.desc,
		Fields:    []string{"sys_id", "name", "table", "active", "user", "group", "order"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sysrule_assignment", opts)
	if err != nil {
		return fmt.Errorf("failed to list assignment rules: %w", err)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledAssignmentRules(cmd, records, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownAssignmentRules(cmd, records)
	}

	var data []map[string]any
	for _, r := range records {
		data = append(data, map[string]any{
			"sys_id":       getStringField(r, "sys_id"),
			"name":         getStringField(r, "name"),
			"table":        getStringField(r, "table"),
			"active":       getFieldValue(r, "active"),
			"assign_to":    getStringField(r, "user"),
			"assign_group": getStringField(r, "group"),
			"order":        getFieldValue(r, "order"),
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d assignment rules", len(records))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "show", Cmd: "jsn assignment-rules <name>", Description: "Show rule details"},
		),
	)
}

// runAssignmentRulesShow executes the assignment-rules show command.
func runAssignmentRulesShow(cmd *cobra.Command, name string) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	outputWriter := appCtx.Output.(*output.Writer)
	sdkClient := appCtx.SDK.(*sdk.Client)
	cfg := appCtx.Config.(*config.Config)
	profile := cfg.GetActiveProfile()
	instanceURL := ""
	if profile != nil {
		instanceURL = profile.InstanceURL
	}

	var query string
	if looksLikeSysID(name) {
		query = fmt.Sprintf("sys_id=%s", name)
	} else {
		query = fmt.Sprintf("name=%s", name)
	}

	opts := &sdk.ListRecordsOptions{
		Limit: 1,
		Query: query,
		// Request specific fields instead of "*" which ServiceNow doesn't support
		Fields: []string{"sys_id", "name", "active", "table", "user", "group", "order", "condition", "script", "description", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sysrule_assignment", opts)
	if err != nil {
		return fmt.Errorf("failed to get assignment rule: %w", err)
	}

	if len(records) == 0 {
		return output.ErrNotFound(fmt.Sprintf("assignment rule '%s' not found", name))
	}

	rule := records[0]

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledAssignmentRule(cmd, rule, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownAssignmentRule(cmd, rule, instanceURL)
	}

	return outputWriter.OK(rule,
		output.WithSummary(fmt.Sprintf("Assignment rule: %s", getStringField(rule, "name"))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "list", Cmd: "jsn assignment-rules", Description: "List all rules"},
		),
	)
}

// pickAssignmentRulePaginated shows a paginated interactive picker for assignment rules.
func pickAssignmentRulePaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
	fetcher := func(ctx context.Context, offset, limit int, searchQuery string) (*tui.PageResult, error) {
		finalQuery := query
		if searchQuery != "" {
			searchPart := "nameLIKE" + searchQuery
			if finalQuery != "" {
				finalQuery = finalQuery + "^" + searchPart
			} else {
				finalQuery = searchPart
			}
		}
		opts := &sdk.ListRecordsOptions{
			Limit:     limit,
			Offset:    offset,
			Query:     finalQuery,
			OrderBy:   orderBy,
			OrderDesc: orderDesc,
			Fields:    []string{"sys_id", "name", "table", "active", "user", "group"},
		}
		records, err := sdkClient.ListRecords(ctx, "sysrule_assignment", opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, r := range records {
			name := getStringField(r, "name")
			table := getStringField(r, "table")
			active := getFieldValue(r, "active")
			assignTo := getStringField(r, "user")
			assignGroup := getStringField(r, "group")

			desc := table
			if active == false || active == "false" {
				desc += " (inactive)"
			}
			if assignTo != "" {
				desc += " → " + assignTo
			} else if assignGroup != "" {
				desc += " → " + assignGroup
			}

			items = append(items, tui.PickerItem{
				ID:          getStringField(r, "sys_id"),
				Title:       name,
				Description: desc,
			})
		}

		hasMore := len(records) >= limit
		return &tui.PageResult{
			Items:   items,
			HasMore: hasMore,
		}, nil
	}

	selected, err := tui.PickWithQueryablePagination("Select an assignment rule:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}
	return selected.ID, nil
}

// printStyledAssignmentRules outputs styled assignment rules list.
func printStyledAssignmentRules(cmd *cobra.Command, rules []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	activeStyle := lipgloss.NewStyle()
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Assignment Rules"))
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-20s %-12s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Table"),
		headerStyle.Render("Status"),
		headerStyle.Render("Assign To"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	for _, r := range rules {
		name := getStringField(r, "name")
		table := getStringField(r, "table")
		active := getFieldValue(r, "active")
		assignTo := getStringField(r, "user")
		assignGroup := getStringField(r, "group")

		status := "Active"
		statusStyle := activeStyle
		if active == false || active == "false" {
			status = "Inactive"
			statusStyle = inactiveStyle
		}

		assignee := assignTo
		if assignee == "" {
			assignee = assignGroup
		}
		if assignee == "" {
			assignee = "-"
		}

		if len(name) > 30 {
			name = name[:27] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-20s %-12s %s\n",
			name,
			mutedStyle.Render(table),
			statusStyle.Render(status),
			mutedStyle.Render(assignee),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn assignment-rules <name>",
		mutedStyle.Render("Show rule details"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownAssignmentRules outputs markdown assignment rules list.
func printMarkdownAssignmentRules(cmd *cobra.Command, rules []map[string]interface{}) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Assignment Rules**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Table | Status | Assign To |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|-------|--------|-----------|")

	for _, r := range rules {
		name := getStringField(r, "name")
		table := getStringField(r, "table")
		active := getFieldValue(r, "active")
		assignTo := getStringField(r, "user")
		assignGroup := getStringField(r, "group")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		assignee := assignTo
		if assignee == "" {
			assignee = assignGroup
		}
		if assignee == "" {
			assignee = "-"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s |\n", name, table, status, assignee)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledAssignmentRule outputs styled assignment rule details.
func printStyledAssignmentRule(cmd *cobra.Command, rule map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(getStringField(rule, "name")))
	fmt.Fprintln(cmd.OutOrStdout())

	// Add link to record
	if instanceURL != "" {
		sysID := getStringField(rule, "sys_id")
		link := fmt.Sprintf("%s/sysrule_assignment.do?sys_id=%s", instanceURL, sysID)
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Link:"),
			fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", link, mutedStyle.Render("Open in Instance")),
		)
		fmt.Fprintln(cmd.OutOrStdout())
	}

	fields := []struct {
		label string
		key   string
	}{
		{"Table:", "table"},
		{"Active:", "active"},
		{"Order:", "order"},
		{"Assign To:", "user"},
		{"Assign Group:", "group"},
		{"Condition:", "condition"},
		{"Description:", "description"},
	}

	for _, f := range fields {
		value := formatValue(rule[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render(f.label),
				valueStyle.Render(value),
			)
		}
	}

	// Show script if present
	script := getStringField(rule, "script")
	if script != "" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), labelStyle.Render("Script:"))
		// Show first few lines of script
		lines := strings.Split(script, "\n")
		for i, line := range lines {
			if i >= 10 {
				fmt.Fprintln(cmd.OutOrStdout(), "  "+mutedStyle.Render("..."))
				break
			}
			if len(line) > 70 {
				line = line[:67] + "..."
			}
			fmt.Fprintln(cmd.OutOrStdout(), "  "+valueStyle.Render(line))
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn assignment-rules",
		labelStyle.Render("List all rules"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownAssignmentRule outputs markdown assignment rule details.
func printMarkdownAssignmentRule(cmd *cobra.Command, rule map[string]interface{}, instanceURL string) error {
	name := getStringField(rule, "name")
	sysID := getStringField(rule, "sys_id")

	// Add link to record
	if instanceURL != "" {
		link := fmt.Sprintf("%s/sysrule_assignment.do?sys_id=%s", instanceURL, sysID)
		fmt.Fprintf(cmd.OutOrStdout(), "**[%s](%s)**\n\n", name, link)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", name)
	}

	fields := []struct {
		label string
		key   string
	}{
		{"Table", "table"},
		{"Active", "active"},
		{"Order", "order"},
		{"Assign To", "user"},
		{"Assign Group", "group"},
		{"Condition", "condition"},
		{"Description", "description"},
	}

	for _, f := range fields {
		value := formatValue(rule[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s:** %s\n", f.label, value)
		}
	}

	// Show script if present
	script := getStringField(rule, "script")
	if script != "" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "**Script:**")
		fmt.Fprintln(cmd.OutOrStdout(), "```javascript")
		fmt.Fprintln(cmd.OutOrStdout(), script)
		fmt.Fprintln(cmd.OutOrStdout(), "```")
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
