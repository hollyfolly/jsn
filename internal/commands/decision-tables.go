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

// decisionTablesFlags holds the flags for the decision-tables command.
type decisionTablesFlags struct {
	limit  int
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
}

// NewDecisionTablesCmd creates the decision-tables command.
func NewDecisionTablesCmd() *cobra.Command {
	var flags decisionTablesFlags

	cmd := &cobra.Command{
		Use:   "decision-tables [<name_or_sys_id>]",
		Short: "Manage decision tables (sys_decision)",
		Long: `List and inspect ServiceNow decision tables.

Decision tables provide business logic without code, commonly used in Flow Designer.

Usage:
  jsn decision-tables <name_or_sys_id>              Show table details
  jsn decision-tables --search <term>               Fuzzy search on name
  jsn decision-tables --active                      Show only active tables

Examples:
  jsn decision-tables "Approval Matrix"
  jsn decision-tables --search "priority" --active`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runDecisionTablesShow(cmd, args[0])
			}
			return runDecisionTablesList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of tables to fetch")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active tables")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all tables (no limit)")

	return cmd
}

// runDecisionTablesList executes the decision-tables list command.
func runDecisionTablesList(cmd *cobra.Command, flags decisionTablesFlags) error {
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
	if flags.search != "" {
		queryParts = append(queryParts, fmt.Sprintf("nameLIKE%s", flags.search))
	}
	if flags.query != "" {
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sys_decision"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		selected, err := pickDecisionTablePaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selected == "" {
			return fmt.Errorf("no table selected")
		}
		return runDecisionTablesShow(cmd, selected)
	}

	// Non-interactive
	limit := flags.limit
	if flags.all {
		limit = 0
	}

	opts := &sdk.ListRecordsOptions{
		Limit:     limit,
		Query:     sysparmQuery,
		OrderBy:   flags.order,
		OrderDesc: flags.desc,
		Fields:    []string{"sys_id", "name", "active", "description", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_decision", opts)
	if err != nil {
		return fmt.Errorf("failed to list decision tables: %w", err)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledDecisionTables(cmd, records, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownDecisionTables(cmd, records)
	}

	var data []map[string]any
	for _, r := range records {
		data = append(data, map[string]any{
			"sys_id":      getStringField(r, "sys_id"),
			"name":        getStringField(r, "name"),
			"active":      getFieldValue(r, "active"),
			"description": getStringField(r, "description"),
			"scope":       getStringField(r, "sys_scope"),
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d decision tables", len(records))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "show", Cmd: "jsn decision-tables <name>", Description: "Show table details"},
		),
	)
}

// runDecisionTablesShow executes the decision-tables show command.
func runDecisionTablesShow(cmd *cobra.Command, name string) error {
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
		Fields: []string{"sys_id", "name", "active", "description", "sys_scope", "sys_created_on", "sys_updated_on", "answer_table", "answer_type"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_decision", opts)
	if err != nil {
		return fmt.Errorf("failed to get decision table: %w", err)
	}

	if len(records) == 0 {
		return output.ErrNotFound(fmt.Sprintf("decision table '%s' not found", name))
	}

	table := records[0]
	sysID := getStringField(table, "sys_id")

	// Fetch decision questions for this table
	questionsOpts := &sdk.ListRecordsOptions{
		Limit:   100,
		Query:   fmt.Sprintf("decision_table=%s", sysID),
		OrderBy: "order",
		Fields:  []string{"sys_id", "name", "label", "type", "order", "reference", "active", "description", "condition", "answer", "default_answer"},
	}
	questions, _ := sdkClient.ListRecords(cmd.Context(), "sys_decision_question", questionsOpts)

	// Fetch decision answers if answer_table is specified
	var answers []map[string]interface{}
	answerTable := getStringField(table, "answer_table")
	if answerTable != "" {
		answersOpts := &sdk.ListRecordsOptions{
			Limit:  100,
			Query:  fmt.Sprintf("decision=%s", sysID),
			Fields: []string{"sys_id", "name", "active", "order"},
		}
		answers, _ = sdkClient.ListRecords(cmd.Context(), answerTable, answersOpts)
	}

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledDecisionTable(cmd, table, questions, answers, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownDecisionTable(cmd, table, questions, answers, instanceURL)
	}

	return outputWriter.OK(map[string]any{
		"table":     table,
		"questions": questions,
		"answers":   answers,
	},
		output.WithSummary(fmt.Sprintf("Decision table: %s", getStringField(table, "name"))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "list", Cmd: "jsn decision-tables", Description: "List all tables"},
		),
	)
}

// pickDecisionTablePaginated shows a paginated interactive picker for decision tables.
func pickDecisionTablePaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
	fetcher := func(ctx context.Context, offset, limit int) (*tui.PageResult, error) {
		opts := &sdk.ListRecordsOptions{
			Limit:     limit,
			Offset:    offset,
			Query:     query,
			OrderBy:   orderBy,
			OrderDesc: orderDesc,
			Fields:    []string{"sys_id", "name", "active", "description"},
		}
		records, err := sdkClient.ListRecords(ctx, "sys_decision", opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, r := range records {
			name := getStringField(r, "name")
			active := getFieldValue(r, "active")
			desc := getStringField(r, "description")

			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			if active == false || active == "false" {
				desc = "[inactive] " + desc
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

	selected, err := tui.PickWithPagination("Select a decision table:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}
	return selected.ID, nil
}

// printStyledDecisionTables outputs styled decision tables list.
func printStyledDecisionTables(cmd *cobra.Command, tables []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Decision Tables"))
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-12s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Status"),
		headerStyle.Render("Description"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	for _, t := range tables {
		name := getStringField(t, "name")
		active := getFieldValue(t, "active")
		desc := getStringField(t, "description")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-12s %s\n",
			name,
			mutedStyle.Render(status),
			mutedStyle.Render(desc),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn decision-tables <name>",
		mutedStyle.Render("Show table details"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownDecisionTables outputs markdown decision tables list.
func printMarkdownDecisionTables(cmd *cobra.Command, tables []map[string]interface{}) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Decision Tables**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Status | Description |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|--------|-------------|")

	for _, t := range tables {
		name := getStringField(t, "name")
		active := getFieldValue(t, "active")
		desc := getStringField(t, "description")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s |\n", name, status, desc)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledDecisionTable outputs styled decision table details.
func printStyledDecisionTable(cmd *cobra.Command, table map[string]interface{}, questions, answers []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(getStringField(table, "name")))
	fmt.Fprintln(cmd.OutOrStdout())

	fields := []struct {
		label string
		key   string
	}{
		{"Active:", "active"},
		{"Description:", "description"},
		{"Answer Table:", "answer_table"},
		{"Answer Type:", "answer_type"},
		{"Scope:", "sys_scope"},
	}

	for _, f := range fields {
		value := formatValue(table[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render(f.label),
				valueStyle.Render(value),
			)
		}
	}

	// Show Studio link
	if instanceURL != "" {
		sysID := getStringField(table, "sys_id")
		studioLink := fmt.Sprintf("%s/now/workflow-studio/builder?table=sys_decision&sysId=%s", instanceURL, sysID)
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Studio:"),
			fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", studioLink, mutedStyle.Render("Open in Studio")),
		)
	}

	// Show questions
	if len(questions) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Decision Rules:"))
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, q := range questions {
			label := getStringField(q, "label")
			order := getFieldValue(q, "order")
			active := getFieldValue(q, "active")
			isDefault := getFieldValue(q, "default_answer")
			condition := getStringField(q, "condition")
			answer := getStringField(q, "answer")

			// Build status indicators
			indicators := ""
			if active == false || active == "false" {
				indicators += " [inactive]"
			}
			if isDefault == true || isDefault == "true" {
				indicators += " [default]"
			}

			// Show rule header
			fmt.Fprintf(cmd.OutOrStdout(), "  %s. %s%s\n",
				mutedStyle.Render(fmt.Sprintf("%v", order)),
				headerStyle.Render(label),
				mutedStyle.Render(indicators))

			// Show condition if present
			if condition != "" {
				// Parse and format the condition
				formattedCondition := formatDecisionCondition(condition)
				fmt.Fprintf(cmd.OutOrStdout(), "      %s: %s\n",
					mutedStyle.Render("When"),
					valueStyle.Render(formattedCondition))
			}

			// Show answer if present
			if answer != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "      %s: %s\n",
					mutedStyle.Render("Then"),
					valueStyle.Render(answer[:8]+"..."))
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	// Show answers
	if len(answers) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Answers:"))
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
		for _, a := range answers {
			name := getStringField(a, "name")
			active := getFieldValue(a, "active")

			status := ""
			if active == false || active == "false" {
				status = " [inactive]"
			}

			fmt.Fprintf(cmd.OutOrStdout(), "  %s%s\n",
				name,
				mutedStyle.Render(status))
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn decision-tables",
		labelStyle.Render("List all tables"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownDecisionTable outputs markdown decision table details.
func printMarkdownDecisionTable(cmd *cobra.Command, table map[string]interface{}, questions, answers []map[string]interface{}, instanceURL string) error {
	name := getStringField(table, "name")
	sysID := getStringField(table, "sys_id")

	// Add Studio link if available
	if instanceURL != "" {
		studioLink := fmt.Sprintf("%s/now/workflow-studio/builder?table=sys_decision&sysId=%s", instanceURL, sysID)
		fmt.Fprintf(cmd.OutOrStdout(), "**[%s](%s)**\n\n", name, studioLink)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", name)
	}

	fields := []struct {
		label string
		key   string
	}{
		{"Active", "active"},
		{"Description", "description"},
		{"Answer Table", "answer_table"},
		{"Answer Type", "answer_type"},
		{"Scope", "sys_scope"},
	}

	for _, f := range fields {
		value := formatValue(table[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s:** %s\n", f.label, value)
		}
	}

	// Show questions
	if len(questions) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "#### Decision Rules")
		fmt.Fprintln(cmd.OutOrStdout())
		for _, q := range questions {
			label := getStringField(q, "label")
			order := getFieldValue(q, "order")
			active := getFieldValue(q, "active")
			isDefault := getFieldValue(q, "default_answer")
			condition := getStringField(q, "condition")
			answer := getStringField(q, "answer")

			status := ""
			if active == false || active == "false" {
				status = " (inactive)"
			}
			if isDefault == true || isDefault == "true" {
				status += " [default]"
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%v. **%s**%s\n", order, label, status)

			if condition != "" {
				formattedCondition := formatDecisionCondition(condition)
				fmt.Fprintf(cmd.OutOrStdout(), "   - **When:** `%s`\n", formattedCondition)
			}

			if answer != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "   - **Then:** `%s...`\n", answer[:8])
			}

			ref := getStringField(q, "reference")
			if ref != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "   - Reference: `%s`\n", ref)
			}
		}
	}

	// Show answers
	if len(answers) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "#### Answers")
		fmt.Fprintln(cmd.OutOrStdout())
		for _, a := range answers {
			name := getStringField(a, "name")
			active := getFieldValue(a, "active")

			status := ""
			if active == false || active == "false" {
				status = " (inactive)"
			}

			fmt.Fprintf(cmd.OutOrStdout(), "- %s%s\n", name, status)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// formatDecisionCondition parses and formats a decision condition for display
// It extracts the human-readable parts from the encoded query format
func formatDecisionCondition(condition string) string {
	if condition == "" {
		return "Always"
	}

	// If it's a simple condition, return it
	if len(condition) < 50 {
		return condition
	}

	// Try to extract the field and operator from simple patterns
	// e.g., "u_imageANYTHING^EQ" -> "u_image is anything"
	if strings.Contains(condition, "ANYTHING") {
		parts := strings.Split(condition, "ANYTHING")
		if len(parts) > 0 {
			field := parts[0]
			return fmt.Sprintf("%s is set", field)
		}
	}

	// Truncate long conditions
	if len(condition) > 60 {
		return condition[:57] + "..."
	}

	return condition
}
