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

// scriptedRestFlags holds the flags for the scripted-rest command.
type scriptedRestFlags struct {
	limit  int
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
}

// NewScriptedRestCmd creates the scripted-rest command.
func NewScriptedRestCmd() *cobra.Command {
	var flags scriptedRestFlags

	cmd := &cobra.Command{
		Use:   "scripted-rest [<name_or_sys_id>]",
		Short: "Manage Scripted REST APIs",
		Long: `List and inspect ServiceNow Scripted REST APIs (sys_ws_definition, sys_ws_operation).

Usage:
  jsn scripted-rest <name_or_sys_id>                Show API details
  jsn scripted-rest --search <term>                Fuzzy search on name
  jsn scripted-rest --active                       Show only active APIs

Examples:
  jsn scripted-rest "My API"
  jsn scripted-rest --search "integration" --active`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runScriptedRestShow(cmd, args[0])
			}
			return runScriptedRestList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of APIs to fetch")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active APIs")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all APIs (no limit)")

	return cmd
}

// runScriptedRestList executes the scripted-rest list command.
func runScriptedRestList(cmd *cobra.Command, flags scriptedRestFlags) error {
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
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sys_ws_definition"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		selected, err := pickScriptedRestPaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selected == "" {
			return fmt.Errorf("no API selected")
		}
		return runScriptedRestShow(cmd, selected)
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
		Fields:    []string{"sys_id", "name", "active", "base_uri", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_ws_definition", opts)
	if err != nil {
		return fmt.Errorf("failed to list scripted REST APIs: %w", err)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledScriptedRest(cmd, records, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownScriptedRest(cmd, records)
	}

	var data []map[string]any
	for _, r := range records {
		data = append(data, map[string]any{
			"sys_id":   getStringField(r, "sys_id"),
			"name":     getStringField(r, "name"),
			"active":   getFieldValue(r, "active"),
			"base_uri": getStringField(r, "base_uri"),
			"scope":    getStringField(r, "sys_scope"),
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d scripted REST APIs", len(records))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "show", Cmd: "jsn scripted-rest <name>", Description: "Show API details"},
		),
	)
}

// runScriptedRestShow executes the scripted-rest show command.
func runScriptedRestShow(cmd *cobra.Command, name string) error {
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
		Fields: []string{"sys_id", "name", "active", "base_uri", "namespace", "service_id", "description", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_ws_definition", opts)
	if err != nil {
		return fmt.Errorf("failed to get scripted REST API: %w", err)
	}

	if len(records) == 0 {
		return output.ErrNotFound(fmt.Sprintf("scripted REST API '%s' not found", name))
	}

	api := records[0]
	sysID := getStringField(api, "sys_id")

	// Fetch operations for this API
	optsOps := &sdk.ListRecordsOptions{
		Limit:  100,
		Query:  fmt.Sprintf("web_service_definition=%s", sysID),
		Fields: []string{"sys_id", "name", "http_method", "route"},
	}
	operations, _ := sdkClient.ListRecords(cmd.Context(), "sys_ws_operation", optsOps)

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledScriptedRestDetail(cmd, api, operations, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownScriptedRestDetail(cmd, api, operations)
	}

	return outputWriter.OK(map[string]any{
		"api":        api,
		"operations": operations,
	},
		output.WithSummary(fmt.Sprintf("Scripted REST API: %s", getStringField(api, "name"))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "list", Cmd: "jsn scripted-rest", Description: "List all APIs"},
		),
	)
}

// pickScriptedRestPaginated shows a paginated interactive picker for scripted REST APIs.
func pickScriptedRestPaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
	fetcher := func(ctx context.Context, offset, limit int) (*tui.PageResult, error) {
		opts := &sdk.ListRecordsOptions{
			Limit:     limit,
			Offset:    offset,
			Query:     query,
			OrderBy:   orderBy,
			OrderDesc: orderDesc,
			Fields:    []string{"sys_id", "name", "active", "base_uri"},
		}
		records, err := sdkClient.ListRecords(ctx, "sys_ws_definition", opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, r := range records {
			name := getStringField(r, "name")
			active := getFieldValue(r, "active")
			baseURI := getStringField(r, "base_uri")

			desc := baseURI
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

	selected, err := tui.PickWithPagination("Select a scripted REST API:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}
	return selected.ID, nil
}

// printStyledScriptedRest outputs styled scripted REST APIs list.
func printStyledScriptedRest(cmd *cobra.Command, apis []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Scripted REST APIs"))
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-12s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Status"),
		headerStyle.Render("Base URI"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	for _, a := range apis {
		name := getStringField(a, "name")
		active := getFieldValue(a, "active")
		baseURI := getStringField(a, "base_uri")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(baseURI) > 40 {
			baseURI = baseURI[:37] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-12s %s\n",
			name,
			mutedStyle.Render(status),
			mutedStyle.Render(baseURI),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn scripted-rest <name>",
		mutedStyle.Render("Show API details & operations"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownScriptedRest outputs markdown scripted REST APIs list.
func printMarkdownScriptedRest(cmd *cobra.Command, apis []map[string]interface{}) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Scripted REST APIs**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Status | Base URI |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|--------|----------|")

	for _, a := range apis {
		name := getStringField(a, "name")
		active := getFieldValue(a, "active")
		baseURI := getStringField(a, "base_uri")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s |\n", name, status, baseURI)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledScriptedRestDetail outputs styled scripted REST API details.
func printStyledScriptedRestDetail(cmd *cobra.Command, api map[string]interface{}, operations []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(getStringField(api, "name")))
	fmt.Fprintln(cmd.OutOrStdout())

	fields := []struct {
		label string
		key   string
	}{
		{"Active:", "active"},
		{"Base URI:", "base_uri"},
		{"Namespace:", "namespace"},
		{"Service ID:", "service_id"},
		{"Description:", "description"},
	}

	for _, f := range fields {
		value := formatValue(api[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render(f.label),
				valueStyle.Render(value),
			)
		}
	}

	// Operations
	if len(operations) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Operations:"))
		for _, op := range operations {
			method := getStringField(op, "http_method")
			route := getStringField(op, "route")
			opName := getStringField(op, "name")
			fmt.Fprintf(cmd.OutOrStdout(), "  %-8s %-30s %s\n",
				labelStyle.Render(method),
				route,
				mutedStyle.Render(opName),
			)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn scripted-rest",
		labelStyle.Render("List all APIs"),
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn rest get \"/api/<namespace>/<service_id>/<route>\"",
		labelStyle.Render("Test operation via REST"),
	)

	// Add hint to view operations via records command
	if instanceURL != "" {
		sysID := getStringField(api, "sys_id")
		fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
			fmt.Sprintf("jsn records --table sys_ws_operation --query \"web_service_definition=%s\"", sysID),
			labelStyle.Render("View all operations"),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownScriptedRestDetail outputs markdown scripted REST API details.
func printMarkdownScriptedRestDetail(cmd *cobra.Command, api map[string]interface{}, operations []map[string]interface{}) error {
	fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", getStringField(api, "name"))

	fields := []struct {
		label string
		key   string
	}{
		{"Active", "active"},
		{"Base URI", "base_uri"},
		{"Namespace", "namespace"},
		{"Service ID", "service_id"},
		{"Description", "description"},
	}

	for _, f := range fields {
		value := formatValue(api[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s:** %s\n", f.label, value)
		}
	}

	if len(operations) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "**Operations:**")
		for _, op := range operations {
			method := getStringField(op, "http_method")
			route := getStringField(op, "route")
			opName := getStringField(op, "name")
			fmt.Fprintf(cmd.OutOrStdout(), "- `%s %s` — %s\n", method, route, opName)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "#### Hints")
	fmt.Fprintln(cmd.OutOrStdout(), "- `jsn scripted-rest` — List all APIs")
	fmt.Fprintln(cmd.OutOrStdout(), "- `jsn rest get \"/api/<namespace>/<service_id>/<route>\"` — Test operation via REST")
	sysID := getStringField(api, "sys_id")
	fmt.Fprintf(cmd.OutOrStdout(), "- `jsn records --table sys_ws_operation --query \"web_service_definition=%s\"` — View all operations\n", sysID)
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
