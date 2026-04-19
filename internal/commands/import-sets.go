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

// importSetsFlags holds the flags for the import-sets command.
type importSetsFlags struct {
	limit  int
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
}

// NewImportSetsCmd creates the import-sets command.
func NewImportSetsCmd() *cobra.Command {
	var flags importSetsFlags

	cmd := &cobra.Command{
		Use:   "import-sets [<name_or_sys_id>]",
		Short: "Manage import sets and transform maps",
		Long: `List and inspect ServiceNow import sets (sys_data_source, sys_transform_map).

Usage:
  jsn import-sets <name_or_sys_id>                 Show import set details
  jsn import-sets --search <term>                 Fuzzy search on name
  jsn import-sets --active                        Show only active sources

Examples:
  jsn import-sets "CSV Import"
  jsn import-sets --search "ldap" --active`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runImportSetsShow(cmd, args[0])
			}
			return runImportSetsList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of sources to fetch")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active sources")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all sources (no limit)")

	return cmd
}

// runImportSetsList executes the import-sets list command.
func runImportSetsList(cmd *cobra.Command, flags importSetsFlags) error {
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
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sys_data_source"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		selected, err := pickImportSetPaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selected == "" {
			return fmt.Errorf("no source selected")
		}
		return runImportSetsShow(cmd, selected)
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
		Fields:    []string{"sys_id", "name", "active", "type", "import_set_table_name", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_data_source", opts)
	if err != nil {
		return fmt.Errorf("failed to list data sources: %w", err)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledImportSets(cmd, records, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownImportSets(cmd, records)
	}

	var data []map[string]any
	for _, r := range records {
		data = append(data, map[string]any{
			"sys_id": getStringField(r, "sys_id"),
			"name":   getStringField(r, "name"),
			"active": getFieldValue(r, "active"),
			"type":   getStringField(r, "type"),
			"table":  getStringField(r, "import_set_table_name"),
			"scope":  getStringField(r, "sys_scope"),
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d data sources", len(records))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "show", Cmd: "jsn import-sets <name>", Description: "Show source details"},
		),
	)
}

// runImportSetsShow executes the import-sets show command.
func runImportSetsShow(cmd *cobra.Command, name string) error {
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
		Fields: []string{"sys_id", "name", "active", "type", "import_set_table_name", "format", "location", "description", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sys_data_source", opts)
	if err != nil {
		return fmt.Errorf("failed to get data source: %w", err)
	}

	if len(records) == 0 {
		return output.ErrNotFound(fmt.Sprintf("data source '%s' not found", name))
	}

	source := records[0]
	importSetTableName := getStringField(source, "import_set_table_name")

	// Fetch transform maps for this source by matching source_table to import_set_table_name
	optsMaps := &sdk.ListRecordsOptions{
		Limit:  100,
		Query:  fmt.Sprintf("source_table=%s", importSetTableName),
		Fields: []string{"sys_id", "name", "active", "source_table", "target_table", "order", "script"},
	}
	transformMaps, _ := sdkClient.ListRecords(cmd.Context(), "sys_transform_map", optsMaps)

	// Fetch transform entries for each transform map
	transformEntries := make(map[string][]map[string]interface{})
	for _, tm := range transformMaps {
		tmID := getStringField(tm, "sys_id")
		optsEntries := &sdk.ListRecordsOptions{
			Limit:   100,
			Query:   fmt.Sprintf("map=%s", tmID),
			OrderBy: "order",
			Fields:  []string{"sys_id", "source_field", "target_field", "coalesce", "choice_action", "use_source_script", "source_script", "order"},
		}
		entries, _ := sdkClient.ListRecords(cmd.Context(), "sys_transform_entry", optsEntries)
		transformEntries[tmID] = entries
	}

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledImportSetDetail(cmd, source, transformMaps, transformEntries, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownImportSetDetail(cmd, source, transformMaps, transformEntries, instanceURL)
	}

	return outputWriter.OK(map[string]any{
		"source":            source,
		"transform_maps":    transformMaps,
		"transform_entries": transformEntries,
	},
		output.WithSummary(fmt.Sprintf("Data source: %s", getStringField(source, "name"))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "list", Cmd: "jsn import-sets", Description: "List all sources"},
		),
	)
}

// pickImportSetPaginated shows a paginated interactive picker for data sources.
func pickImportSetPaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
	fetcher := func(ctx context.Context, offset, limit int) (*tui.PageResult, error) {
		opts := &sdk.ListRecordsOptions{
			Limit:     limit,
			Offset:    offset,
			Query:     query,
			OrderBy:   orderBy,
			OrderDesc: orderDesc,
			Fields:    []string{"sys_id", "name", "active", "type", "import_set_table_name"},
		}
		records, err := sdkClient.ListRecords(ctx, "sys_data_source", opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, r := range records {
			name := getStringField(r, "name")
			active := getFieldValue(r, "active")
			dsType := getStringField(r, "type")
			tableName := getStringField(r, "import_set_table_name")

			desc := dsType
			if tableName != "" {
				desc += " → " + tableName
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

	selected, err := tui.PickWithPagination("Select a data source:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}
	return selected.ID, nil
}

// printStyledImportSets outputs styled import sets list.
func printStyledImportSets(cmd *cobra.Command, sources []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Data Sources"))
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-12s %-15s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Status"),
		headerStyle.Render("Type"),
		headerStyle.Render("Table"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	for _, s := range sources {
		name := getStringField(s, "name")
		active := getFieldValue(s, "active")
		dsType := getStringField(s, "type")
		tableName := getStringField(s, "import_set_table_name")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(dsType) > 12 {
			dsType = dsType[:9] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-12s %-15s %s\n",
			name,
			mutedStyle.Render(status),
			mutedStyle.Render(dsType),
			mutedStyle.Render(tableName),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn import-sets <name>",
		mutedStyle.Render("Show source details"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownImportSets outputs markdown import sets list.
func printMarkdownImportSets(cmd *cobra.Command, sources []map[string]interface{}) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Data Sources**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Status | Type | Table |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|--------|------|-------|")

	for _, s := range sources {
		name := getStringField(s, "name")
		active := getFieldValue(s, "active")
		dsType := getStringField(s, "type")
		tableName := getStringField(s, "import_set_table_name")

		status := "Active"
		if active == false || active == "false" {
			status = "Inactive"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s |\n", name, status, dsType, tableName)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledImportSetDetail outputs styled import set details.
func printStyledImportSetDetail(cmd *cobra.Command, source map[string]interface{}, transformMaps []map[string]interface{}, transformEntries map[string][]map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(getStringField(source, "name")))
	fmt.Fprintln(cmd.OutOrStdout())

	// Add link to import table if available
	importTableName := getStringField(source, "import_set_table_name")
	if instanceURL != "" && importTableName != "" {
		importTableLink := fmt.Sprintf("%s/%s_list.do", instanceURL, importTableName)
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Import Table:"),
			fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", importTableLink, mutedStyle.Render(importTableName)),
		)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Import Table:"),
			valueStyle.Render(importTableName),
		)
	}

	fields := []struct {
		label string
		key   string
	}{
		{"Active:", "active"},
		{"Type:", "type"},
		{"Format:", "format"},
		{"Location:", "location"},
		{"Description:", "description"},
	}

	for _, f := range fields {
		value := formatValue(source[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render(f.label),
				valueStyle.Render(value),
			)
		}
	}

	// Transform Maps
	if len(transformMaps) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Transform Maps:"))
		for _, tm := range transformMaps {
			tmID := getStringField(tm, "sys_id")
			name := getStringField(tm, "name")
			sourceTable := getStringField(tm, "source_table")
			targetTable := getStringField(tm, "target_table")

			fmt.Fprintf(cmd.OutOrStdout(), "  %-30s %s → %s\n",
				name,
				mutedStyle.Render(sourceTable),
				mutedStyle.Render(targetTable),
			)

			// Show transform entries for this map
			if entries, ok := transformEntries[tmID]; ok && len(entries) > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintf(cmd.OutOrStdout(), "    %-20s %-20s %-10s %-10s\n",
					"Source Field", "Target Field", "Coalesce", "Action")
				for _, entry := range entries {
					sourceField := getStringField(entry, "source_field")
					targetField := getStringField(entry, "target_field")
					coalesce := getStringField(entry, "coalesce")
					choiceAction := getStringField(entry, "choice_action")
					useSourceScript := getFieldValue(entry, "use_source_script")

					if coalesce == "" || coalesce == "false" {
						coalesce = "-"
					} else {
						coalesce = "Yes"
					}
					if choiceAction == "" {
						choiceAction = "-"
					}

					fmt.Fprintf(cmd.OutOrStdout(), "    %-20s %-20s %-10s %-10s\n",
						sourceField,
						targetField,
						mutedStyle.Render(coalesce),
						mutedStyle.Render(choiceAction),
					)

					// Show source script if present
					if useSourceScript == true || useSourceScript == "true" {
						sourceScript := getStringField(entry, "source_script")
						if sourceScript != "" {
							fmt.Fprintln(cmd.OutOrStdout(), "    "+mutedStyle.Render("  Script:"))
							lines := strings.Split(sourceScript, "\n")
							for i, line := range lines {
								if i >= 3 {
									fmt.Fprintln(cmd.OutOrStdout(), "      "+mutedStyle.Render("..."))
									break
								}
								if len(line) > 60 {
									line = line[:57] + "..."
								}
								fmt.Fprintln(cmd.OutOrStdout(), "      "+valueStyle.Render(line))
							}
						}
					}
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn import-sets",
		labelStyle.Render("List all sources"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownImportSetDetail outputs markdown import set details.
func printMarkdownImportSetDetail(cmd *cobra.Command, source map[string]interface{}, transformMaps []map[string]interface{}, transformEntries map[string][]map[string]interface{}, instanceURL string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", getStringField(source, "name"))

	// Add link to import table if available
	importTableName := getStringField(source, "import_set_table_name")
	if instanceURL != "" && importTableName != "" {
		importTableLink := fmt.Sprintf("%s/%s_list.do", instanceURL, importTableName)
		fmt.Fprintf(cmd.OutOrStdout(), "- **Import Table:** [%s](%s)\n", importTableName, importTableLink)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "- **Import Table:** %s\n", importTableName)
	}

	fields := []struct {
		label string
		key   string
	}{
		{"Active", "active"},
		{"Type", "type"},
		{"Format", "format"},
		{"Description", "description"},
	}

	for _, f := range fields {
		value := formatValue(source[f.key])
		if value != "" && value != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s:** %s\n", f.label, value)
		}
	}

	if len(transformMaps) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "**Transform Maps:**")
		for _, tm := range transformMaps {
			tmID := getStringField(tm, "sys_id")
			name := getStringField(tm, "name")
			sourceTable := getStringField(tm, "source_table")
			targetTable := getStringField(tm, "target_table")
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s:** %s → %s\n", name, sourceTable, targetTable)

			// Show transform entries for this map
			if entries, ok := transformEntries[tmID]; ok && len(entries) > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), "  | Source Field | Target Field | Coalesce | Choice Action |")
				fmt.Fprintln(cmd.OutOrStdout(), "  |--------------|--------------|----------|---------------|")
				for _, entry := range entries {
					sourceField := getStringField(entry, "source_field")
					targetField := getStringField(entry, "target_field")
					coalesce := getStringField(entry, "coalesce")
					choiceAction := getStringField(entry, "choice_action")
					useSourceScript := getFieldValue(entry, "use_source_script")

					if coalesce == "" || coalesce == "false" {
						coalesce = "-"
					}
					if choiceAction == "" {
						choiceAction = "-"
					}

					fmt.Fprintf(cmd.OutOrStdout(), "  | %s | %s | %s | %s |\n",
						sourceField, targetField, coalesce, choiceAction)

					// Show source script if present
					if useSourceScript == true || useSourceScript == "true" {
						sourceScript := getStringField(entry, "source_script")
						if sourceScript != "" {
							fmt.Fprintln(cmd.OutOrStdout(), "  | *Script:* | | | |")
							fmt.Fprintln(cmd.OutOrStdout(), "```javascript")
							fmt.Fprintln(cmd.OutOrStdout(), sourceScript)
							fmt.Fprintln(cmd.OutOrStdout(), "```")
						}
					}
				}
			}
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
