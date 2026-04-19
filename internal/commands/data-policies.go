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

// dataPoliciesFlags holds the flags for the data-policies command.
type dataPoliciesFlags struct {
	limit  int
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
}

// NewDataPoliciesCmd creates the data-policies command.
func NewDataPoliciesCmd() *cobra.Command {
	var flags dataPoliciesFlags

	cmd := &cobra.Command{
		Use:   "data-policies [<name_or_sys_id>]",
		Short: "Manage data policies",
		Long: `List and inspect ServiceNow data policies (sys_data_policy2).

Data policies enforce field-level security server-side, complementing UI policies.

Usage:
  jsn data-policies <name_or_sys_id>               Show policy details
  jsn data-policies --search <term>               Fuzzy search on name
  jsn data-policies --active                      Show only active policies

Examples:
  jsn data-policies "Incident Policy"
  jsn data-policies --search "mandatory" --active`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runDataPoliciesShow(cmd, args[0])
			}
			return runDataPoliciesList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of policies to fetch")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active policies")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all policies (no limit)")

	return cmd
}

// runDataPoliciesList executes the data-policies list command.
func runDataPoliciesList(cmd *cobra.Command, flags dataPoliciesFlags) error {
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
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sys_data_policy2"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		selected, err := pickDataPolicyPaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selected == "" {
			return fmt.Errorf("no policy selected")
		}
		return runDataPoliciesShow(cmd, selected)
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
		Fields:    []string{"sys_id", "short_description", "model_table", "inherit", "reverse_if_false", "active", "sys_scope", "apply_import_set", "apply_soap", "enforce_ui", "description", "conditions"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_data_policy2", opts)
	if err != nil {
		return fmt.Errorf("failed to list data policies: %w", err)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledDataPolicies(cmd, records, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownDataPolicies(cmd, records)
	}

	var data []map[string]any
	for _, r := range records {
		data = append(data, map[string]any{
			"sys_id":           getStringField(r, "sys_id"),
			"name":             getStringField(r, "short_description"),
			"table":            getStringField(r, "model_table"),
			"inherit":          getFieldValue(r, "inherit"),
			"reverse_if_false": getFieldValue(r, "reverse_if_false"),
			"active":           getFieldValue(r, "active"),
			"scope":            getStringField(r, "sys_scope"),
			"apply_import_set": getFieldValue(r, "apply_import_set"),
			"apply_soap":       getFieldValue(r, "apply_soap"),
			"enforce_ui":       getFieldValue(r, "enforce_ui"),
			"description":      getStringField(r, "description"),
			"conditions":       getStringField(r, "conditions"),
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d data policies", len(records))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "show", Cmd: "jsn data-policies <name>", Description: "Show policy details"},
		),
	)
}

// runDataPoliciesShow executes the data-policies show command.
func runDataPoliciesShow(cmd *cobra.Command, name string) error {
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
		Fields: []string{"sys_id", "short_description", "model_table", "inherit", "reverse_if_false", "active", "sys_scope", "apply_import_set", "apply_soap", "enforce_ui", "description", "conditions"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_data_policy2", opts)
	if err != nil {
		return fmt.Errorf("failed to get data policy: %w", err)
	}

	if len(records) == 0 {
		return output.ErrNotFound(fmt.Sprintf("data policy '%s' not found", name))
	}

	policy := records[0]
	sysID := getStringField(policy, "sys_id")

	// Fetch policy rules
	optsRules := &sdk.ListRecordsOptions{
		Limit:  100,
		Query:  fmt.Sprintf("sys_data_policy=%s", sysID),
		Fields: []string{"sys_id", "field", "mandatory", "read_only", "visible"},
	}
	rules, _ := sdkClient.ListRecords(cmd.Context(), "sys_data_policy_rule", optsRules)

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledDataPolicyDetail(cmd, policy, rules, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownDataPolicyDetail(cmd, policy, rules, instanceURL)
	}

	return outputWriter.OK(map[string]any{
		"policy": policy,
		"rules":  rules,
	},
		output.WithSummary(fmt.Sprintf("Data policy: %s", getStringField(policy, "short_description"))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "list", Cmd: "jsn data-policies", Description: "List all policies"},
		),
	)
}

// pickDataPolicyPaginated shows a paginated interactive picker for data policies.
func pickDataPolicyPaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
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
			Fields:    []string{"sys_id", "short_description", "active", "model_table", "description"},
		}
		records, err := sdkClient.ListRecords(ctx, "sys_data_policy2", opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, r := range records {
			name := getStringField(r, "short_description")
			table := getStringField(r, "model_table")
			active := getFieldValue(r, "active")
			desc := getStringField(r, "description")

			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			if table != "" {
				desc = table + " | " + desc
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

	selected, err := tui.PickWithQueryablePagination("Select a data policy:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}
	return selected.ID, nil
}

// printStyledDataPolicies outputs styled data policies list.
func printStyledDataPolicies(cmd *cobra.Command, policies []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Data Policies"))
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-20s %-12s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Table"),
		headerStyle.Render("Status"),
		headerStyle.Render("Description"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	for _, p := range policies {
		name := getStringField(p, "short_description")
		table := getStringField(p, "model_table")
		active := getFieldValue(p, "active")
		desc := getStringField(p, "description")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(desc) > 30 {
			desc = desc[:27] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-20s %-12s %s\n",
			name,
			mutedStyle.Render(table),
			mutedStyle.Render(status),
			mutedStyle.Render(desc),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn data-policies <name>",
		mutedStyle.Render("Show policy details"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownDataPolicies outputs markdown data policies list.
func printMarkdownDataPolicies(cmd *cobra.Command, policies []map[string]interface{}) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Data Policies**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Table | Status | Description |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|-------|--------|-------------|")

	for _, p := range policies {
		name := getStringField(p, "short_description")
		table := getStringField(p, "model_table")
		active := getFieldValue(p, "active")
		desc := getStringField(p, "description")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s |\n", name, table, status, desc)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledDataPolicyDetail outputs styled data policy details.
func printStyledDataPolicyDetail(cmd *cobra.Command, policy map[string]interface{}, rules []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(getStringField(policy, "short_description")))
	fmt.Fprintln(cmd.OutOrStdout())

	fields := []struct {
		label string
		key   string
	}{
		{"Table:", "model_table"},
		{"Inherit:", "inherit"},
		{"Reverse if False:", "reverse_if_false"},
		{"Active:", "active"},
		{"Apply Import Set:", "apply_import_set"},
		{"Apply SOAP:", "apply_soap"},
		{"Enforce UI:", "enforce_ui"},
		{"Conditions:", "conditions"},
		{"Description:", "description"},
		{"Scope:", "sys_scope"},
	}

	for _, f := range fields {
		value := formatValue(policy[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render(f.label),
				valueStyle.Render(value),
			)
		}
	}

	// Related Records Link
	if instanceURL != "" {
		sysID := getStringField(policy, "sys_id")
		rulesLink := fmt.Sprintf("%s/sys_data_policy_rule_list.do?sysparm_query=sys_data_policy=%s", instanceURL, sysID)
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Field Rules:"),
			fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", rulesLink, mutedStyle.Render("View in List")),
		)
	}

	// Rules
	if len(rules) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Field Rules:"))
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %-10s %-10s %-10s\n",
			"Field", "Mandatory", "Read Only", "Visible")
		for _, r := range rules {
			field := getStringField(r, "field")
			mandatory := getStringField(r, "mandatory")
			readOnly := getStringField(r, "read_only")
			visible := getStringField(r, "visible")

			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %-10s %-10s %-10s\n",
				field,
				mutedStyle.Render(mandatory),
				mutedStyle.Render(readOnly),
				mutedStyle.Render(visible),
			)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn data-policies",
		labelStyle.Render("List all policies"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownDataPolicyDetail outputs markdown data policy details.
func printMarkdownDataPolicyDetail(cmd *cobra.Command, policy map[string]interface{}, rules []map[string]interface{}, instanceURL string) error {
	name := getStringField(policy, "short_description")
	sysID := getStringField(policy, "sys_id")

	fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", name)

	fields := []struct {
		label string
		key   string
	}{
		{"Table", "model_table"},
		{"Inherit", "inherit"},
		{"Reverse if False", "reverse_if_false"},
		{"Active", "active"},
		{"Apply Import Set", "apply_import_set"},
		{"Apply SOAP", "apply_soap"},
		{"Enforce UI", "enforce_ui"},
		{"Conditions", "conditions"},
		{"Description", "description"},
		{"Scope", "sys_scope"},
	}

	for _, f := range fields {
		value := formatValue(policy[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s:** %s\n", f.label, value)
		}
	}

	// Related Records Link
	if instanceURL != "" {
		rulesLink := fmt.Sprintf("%s/sys_data_policy_rule_list.do?sysparm_query=sys_data_policy=%s", instanceURL, sysID)
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "- **Field Rules:** [View in List](%s)\n", rulesLink)
	}

	if len(rules) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "**Field Rules:**")
		fmt.Fprintln(cmd.OutOrStdout(), "| Field | Mandatory | Read Only | Visible |")
		fmt.Fprintln(cmd.OutOrStdout(), "|-------|-----------|-----------|---------|")
		for _, r := range rules {
			field := getStringField(r, "field")
			mandatory := getStringField(r, "mandatory")
			readOnly := getStringField(r, "read_only")
			visible := getStringField(r, "visible")
			fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s |\n", field, mandatory, readOnly, visible)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
