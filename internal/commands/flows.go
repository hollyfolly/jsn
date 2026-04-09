package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jacebenson/jsn/internal/appctx"
	"github.com/jacebenson/jsn/internal/config"
	"github.com/jacebenson/jsn/internal/output"
	"github.com/jacebenson/jsn/internal/sdk"
	"github.com/jacebenson/jsn/internal/tui"
	"github.com/spf13/cobra"
)

// flowsListFlags holds the flags for the flows command.
type flowsListFlags struct {
	limit  int
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
	debug  bool
}

// NewFlowsCmd creates the flows command.
func NewFlowsCmd() *cobra.Command {
	var flags flowsListFlags

	cmd := &cobra.Command{
		Use:   "flows [<name_or_sys_id>] [variables]",
		Short: "Manage Flow Designer flows",
		Long: `List, inspect, and create ServiceNow Flow Designer flows.

Usage:
  jsn flows                                    Interactive picker (TTY) or usage info
  jsn flows <name_or_sys_id>                   Show flow details
  jsn flows <name_or_sys_id> variables         Show flow variables only
  jsn flows create [flags]                     Create a new flow or subflow
  jsn flows --search <term>                    Fuzzy search on name (LIKE match)
  jsn flows --query <encoded_query>            Raw ServiceNow encoded query filter

Filtering:
  --search <term>   Fuzzy search on name (LIKE match)
  --query <query>   Raw ServiceNow encoded query for advanced filtering
  --active          Show only active flows

Subcommands:
  create            Create a new flow or subflow
  execute           Execute/test a flow
  executions        Show flow execution history

Examples:
  jsn flows "Approval Flow"
  jsn flows --search approval
  jsn flows --active --json
  jsn flows --query "nameLIKEapproval^active=true" --limit 50
  jsn flows create --name "My Flow" --type flow
  jsn flows create --name "My Helper" --type subflow --input "id:string:ID:true"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mode 1: Direct lookup by name or sys_id
			if len(args) > 0 {
				name := args[0]
				showVariables := len(args) > 1 && args[1] == "variables"
				return runFlowsShow(cmd, name, showVariables)
			}

			// Mode 2 & 3: Search/list (handles interactive picker when no filters)
			return runFlowsList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of flows to fetch")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active flows")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all flows (no limit)")
	cmd.Flags().BoolVar(&flags.debug, "debug", false, "Show debug info including raw gzipped values")

	cmd.AddCommand(
		newFlowsExecutionsCmd(),
		newFlowsExecuteCmd(),
		newFlowsCreateCmd(),
		newFlowsAddTriggerCmd(),
	)

	return cmd
}

// runFlowsList executes the flows list command.
func runFlowsList(cmd *cobra.Command, flags flowsListFlags) error {
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
		// Wrap simple queries with table-specific display column
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sys_hub_flow"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	// Set limit
	limit := flags.limit
	if flags.all {
		limit = 0
	}

	opts := &sdk.ListFlowsOptions{
		Limit:     limit,
		Query:     sysparmQuery,
		OrderBy:   flags.order,
		OrderDesc: flags.desc,
	}

	flows, err := sdkClient.ListFlows(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("failed to list flows: %w", err)
	}

	// Determine output format
	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode - let user select a flow to view (auto-detect TTY)
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		// Use paginated picker for interactive mode
		selectedFlow, err := pickFlowPaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selectedFlow == "" {
			return fmt.Errorf("no flow selected")
		}
		// Show the selected flow
		return runFlowsShow(cmd, selectedFlow, false)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledFlowsList(cmd, flows, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownFlowsList(cmd, flows)
	}

	// Build data for JSON/quiet output
	var data []map[string]any
	for _, flow := range flows {
		row := map[string]any{
			"sys_id":         flow.SysID,
			"name":           flow.Name,
			"active":         flow.Active,
			"scope":          flow.Scope,
			"sys_scope":      flow.SysScope,
			"version":        flow.Version,
			"run_as":         flow.RunAs,
			"sys_updated_on": flow.UpdatedOn,
		}
		if instanceURL != "" {
			row["link"] = fmt.Sprintf("%s/sys_hub_flow.do?sys_id=%s", instanceURL, flow.SysID)
		}
		data = append(data, row)
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d flows", len(flows))),
		output.WithBreadcrumbs(
			output.Breadcrumb{
				Action:      "show",
				Cmd:         "jsn flows <name>",
				Description: "Show flow details",
			},
		),
	)
}

// flowsExecuteFlags holds the flags for the flows execute command.
type flowsExecuteFlags struct {
	inputs map[string]string
}

// newFlowsExecuteCmd creates the flows execute command.
func newFlowsExecuteCmd() *cobra.Command {
	var flags flowsExecuteFlags

	cmd := &cobra.Command{
		Use:   "execute [<flow_name_or_id>]",
		Short: "Execute/test a flow",
		Long: `Manually execute a flow to test it.

If no flow name or sys_id is provided, an interactive picker will help you select one.
Use --input to provide flow input variables.

Examples:
  jsn flows execute "My Flow"
  jsn flows execute "My Flow" --input table=incident --input sys_id=12345`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var identifier string
			if len(args) > 0 {
				identifier = args[0]
			}
			return runFlowsExecute(cmd, identifier, flags)
		},
	}

	cmd.Flags().StringToStringVar(&flags.inputs, "input", nil, "Flow input variables (key=value pairs)")

	return cmd
}

// runFlowsExecute executes the flows execute command.
func runFlowsExecute(cmd *cobra.Command, identifier string, flags flowsExecuteFlags) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	outputWriter := appCtx.Output.(*output.Writer)
	sdkClient := appCtx.SDK.(*sdk.Client)

	// Interactive flow selection if no identifier provided
	if identifier == "" {
		isTerminal := output.IsTTY(cmd.OutOrStdout())
		if !isTerminal {
			return output.ErrUsage("Flow name or sys_id is required in non-interactive mode")
		}

		selectedFlow, err := pickFlow(cmd.Context(), sdkClient, "Select a flow to execute:")
		if err != nil {
			return err
		}
		identifier = selectedFlow
	}

	// Get the flow
	flow, err := sdkClient.GetFlow(cmd.Context(), identifier)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}

	// Convert inputs map to interface map
	inputs := make(map[string]interface{})
	for k, v := range flags.inputs {
		inputs[k] = v
	}

	// Execute the flow
	execInput := sdk.ExecuteFlowInput{
		Inputs: inputs,
	}

	execution, err := sdkClient.ExecuteFlow(cmd.Context(), flow.SysID, execInput)
	if err != nil {
		return fmt.Errorf("failed to execute flow: %w", err)
	}

	return outputWriter.OK(map[string]any{
		"sys_id":    execution.SysID,
		"flow_id":   flow.SysID,
		"flow_name": flow.Name,
		"status":    execution.Status,
		"started":   execution.Started,
	},
		output.WithSummary(fmt.Sprintf("Executed flow '%s'", flow.Name)),
		output.WithBreadcrumbs(
			output.Breadcrumb{
				Action:      "executions",
				Cmd:         fmt.Sprintf("jsn flows executions %s", flow.SysID),
				Description: "View execution history",
			},
		),
	)
}

// printStyledFlowsList outputs styled flows list.
func printStyledFlowsList(cmd *cobra.Command, flows []sdk.Flow, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	activeStyle := lipgloss.NewStyle()
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Flows"))
	fmt.Fprintln(cmd.OutOrStdout())

	// Column headers
	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-36s %-12s %-20s\n",
		headerStyle.Render("Sys ID"),
		headerStyle.Render("Name"),
		headerStyle.Render("Status"),
		headerStyle.Render("Scope"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	// Flows
	for _, flow := range flows {
		status := "Active"
		statusStyle := activeStyle
		if !flow.Active {
			status = "Inactive"
			statusStyle = inactiveStyle
		}

		scope := flow.Scope
		if scope == "" {
			scope = flow.SysScope
		}
		if scope == "" {
			scope = "global"
		}

		name := flow.Name
		if len(name) > 34 {
			name = name[:31] + "..."
		}

		if instanceURL != "" {
			link := fmt.Sprintf("%s/sys_hub_flow.do?sys_id=%s", instanceURL, flow.SysID)
			nameWithLink := fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", link, name)
			fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-36s %-12s %-20s\n",
				mutedStyle.Render(flow.SysID),
				nameWithLink,
				statusStyle.Render(status),
				mutedStyle.Render(scope),
			)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-36s %-12s %-20s\n",
				mutedStyle.Render(flow.SysID),
				name,
				statusStyle.Render(status),
				mutedStyle.Render(scope),
			)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn flows <name>",
		mutedStyle.Render("Show flow details"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownFlowsList outputs markdown flows list.
func printMarkdownFlowsList(cmd *cobra.Command, flows []sdk.Flow) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Flows**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Sys ID | Name | Status | Scope |")
	fmt.Fprintln(cmd.OutOrStdout(), "|--------|------|--------|-------|")

	for _, flow := range flows {
		status := "Active"
		if !flow.Active {
			status = "Inactive"
		}
		scope := flow.Scope
		if scope == "" {
			scope = flow.SysScope
		}
		if scope == "" {
			scope = "global"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s |\n", flow.SysID, flow.Name, status, scope)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// runFlowsShow executes the flows show command.
func runFlowsShow(cmd *cobra.Command, name string, showVariables bool) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	outputWriter := appCtx.Output.(*output.Writer)

	sdkClient := appCtx.SDK.(*sdk.Client)

	// Interactive flow selection if no name provided
	if name == "" {
		isTerminal := output.IsTTY(cmd.OutOrStdout())
		if !isTerminal {
			return output.ErrUsage("Flow name is required in non-interactive mode")
		}

		selectedFlow, err := pickFlow(cmd.Context(), sdkClient, "Select a flow:")
		if err != nil {
			return err
		}
		name = selectedFlow
	}

	// Get the flow first to get the ID
	flow, err := sdkClient.GetFlow(cmd.Context(), name)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}

	// If showing variables only
	if showVariables {
		return showFlowVariables(cmd, flow, sdkClient, outputWriter)
	}

	// Use InspectFlow to get comprehensive flow details
	inspection, err := sdkClient.InspectFlow(cmd.Context(), flow.SysID)
	if err != nil {
		return fmt.Errorf("failed to inspect flow: %w", err)
	}

	// Determine output format
	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Get instance URL for links
	cfg := appCtx.Config.(*config.Config)
	profile := cfg.GetActiveProfile()
	instanceURL := ""
	if profile != nil {
		instanceURL = profile.InstanceURL
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledFlowInspection(cmd, inspection, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownFlowInspection(cmd, inspection)
	}

	// Build data for JSON
	data := map[string]any{
		"flow": map[string]any{
			"sys_id":      inspection.Flow.SysID,
			"name":        inspection.Flow.Name,
			"type":        inspection.Flow.Type,
			"active":      inspection.Flow.Active,
			"description": inspection.Flow.Description,
			"version":     inspection.Flow.Version,
		},
		"components":          inspection.Components,
		"action_instances":    inspection.ActionInstances,
		"action_instances_v2": inspection.ActionInstancesV2,
		"trigger_instances":   inspection.TriggerInstances,
		"flow_inputs":         inspection.FlowInputs,
		"flow_outputs":        inspection.FlowOutputs,
		"version_record":      inspection.Version,
	}
	if instanceURL != "" {
		data["link"] = fmt.Sprintf("%s/sys_hub_flow.do?sys_id=%s", instanceURL, inspection.Flow.SysID)
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("Flow: %s", inspection.Flow.Name)),
		output.WithBreadcrumbs(
			output.Breadcrumb{
				Action:      "list",
				Cmd:         "jsn flows --search <term>",
				Description: "Search flows",
			},
		),
	)
}

// showFlowVariables displays only the flow variables.
func showFlowVariables(cmd *cobra.Command, flow *sdk.Flow, sdkClient *sdk.Client, outputWriter *output.Writer) error {
	// Get flow variables
	variables, err := sdkClient.GetFlowVariables(cmd.Context(), flow.SysID)
	if err != nil {
		return fmt.Errorf("failed to get flow variables: %w", err)
	}

	// Determine output format
	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledFlowVariables(cmd, flow, variables)
	}

	if format == output.FormatMarkdown {
		return printMarkdownFlowVariables(cmd, flow, variables)
	}

	// Build data for JSON
	varData := make([]map[string]any, len(variables))
	for i, v := range variables {
		varData[i] = map[string]any{
			"sys_id": v.SysID,
			"name":   v.Name,
			"type":   v.Type,
			"label":  v.Label,
			"value":  v.Value,
		}
	}

	data := map[string]any{
		"sys_id":    flow.SysID,
		"name":      flow.Name,
		"variables": varData,
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("Flow Variables: %s (%d variables)", flow.Name, len(variables))),
	)
}

// printStyledFlowVariables outputs styled flow variables.
func printStyledFlowVariables(cmd *cobra.Command, flow *sdk.Flow, variables []sdk.FlowVariable) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(fmt.Sprintf("Flow Variables: %s", flow.Name)))
	fmt.Fprintln(cmd.OutOrStdout())

	if len(variables) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), mutedStyle.Render("  No variables defined for this flow."))
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
	}

	// Column headers
	fmt.Fprintf(cmd.OutOrStdout(), "  %-30s %-20s %-30s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Type"),
		headerStyle.Render("Label"),
		headerStyle.Render("Value"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	// Variables
	for _, v := range variables {
		name := v.Name
		if len(name) > 28 {
			name = name[:25] + "..."
		}

		label := v.Label
		if len(label) > 28 {
			label = label[:25] + "..."
		}

		value := v.Value
		if value == "" {
			value = "-"
		}
		if len(value) > 20 {
			value = value[:17] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-30s %-20s %-30s %s\n",
			valueStyle.Render(name),
			mutedStyle.Render(v.Type),
			mutedStyle.Render(label),
			mutedStyle.Render(value),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownFlowVariables outputs markdown flow variables.
func printMarkdownFlowVariables(cmd *cobra.Command, flow *sdk.Flow, variables []sdk.FlowVariable) error {
	fmt.Fprintf(cmd.OutOrStdout(), "**Flow Variables: %s**\n\n", flow.Name)

	if len(variables) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No variables defined for this flow.")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Type | Label | Value |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|------|-------|-------|")

	for _, v := range variables {
		value := v.Value
		if value == "" {
			value = "-"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s |\n", v.Name, v.Type, v.Label, value)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// pickFlow shows an interactive flow picker and returns the selected flow name.
func pickFlow(ctx context.Context, sdkClient *sdk.Client, title string) (string, error) {
	fetcher := func(ctx context.Context, offset, limit int) (*tui.PageResult, error) {
		opts := &sdk.ListFlowsOptions{
			Limit:   limit,
			Offset:  offset,
			OrderBy: "name",
		}
		flows, err := sdkClient.ListFlows(ctx, opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, f := range flows {
			status := "Active"
			if !f.Active {
				status = "Inactive"
			}
			items = append(items, tui.PickerItem{
				ID:          f.Name,
				Title:       f.Name,
				Description: status,
			})
		}

		hasMore := len(flows) >= limit
		return &tui.PageResult{
			Items:   items,
			HasMore: hasMore,
		}, nil
	}

	selected, err := tui.PickWithPagination(title, fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", fmt.Errorf("selection cancelled")
	}

	return selected.ID, nil
}

// pickFlowFromList shows a picker from an already-fetched list of flows.
func pickFlowFromList(flows []sdk.Flow) (string, error) {
	var items []tui.PickerItem
	for _, f := range flows {
		status := "Active"
		if !f.Active {
			status = "Inactive"
		}
		scope := f.Scope
		if scope == "" {
			scope = f.SysScope
		}
		if scope == "" {
			scope = "global"
		}
		items = append(items, tui.PickerItem{
			ID:          f.Name,
			Title:       f.Name,
			Description: fmt.Sprintf("%s - %s", status, scope),
		})
	}

	selected, err := tui.Pick("Select a flow to view:", items, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}

	return selected.ID, nil
}

// pickFlowPaginated shows a paginated interactive picker for flows.
// Fetches pages on demand so the user can scroll through all flows.
func pickFlowPaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
	fetcher := func(ctx context.Context, offset, limit int) (*tui.PageResult, error) {
		opts := &sdk.ListFlowsOptions{
			Limit:     limit,
			Offset:    offset,
			Query:     query,
			OrderBy:   orderBy,
			OrderDesc: orderDesc,
		}
		flows, err := sdkClient.ListFlows(ctx, opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, f := range flows {
			status := "Active"
			if !f.Active {
				status = "Inactive"
			}
			scope := f.Scope
			if scope == "" {
				scope = f.SysScope
			}
			if scope == "" {
				scope = "global"
			}
			items = append(items, tui.PickerItem{
				ID:          f.Name,
				Title:       f.Name,
				Description: fmt.Sprintf("%s - %s", status, scope),
			})
		}

		hasMore := len(flows) >= limit
		return &tui.PageResult{
			Items:   items,
			HasMore: hasMore,
		}, nil
	}

	selected, err := tui.PickWithPagination("Select a flow to view:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}

	return selected.ID, nil
}

// newFlowsExecutionsCmd creates the flows executions command.
func newFlowsExecutionsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "executions [<flow_name>]",
		Short: "Show flow execution history",
		Long: `Display execution history for a Flow Designer flow.

If no flow name is provided, an interactive picker will help you select one.

Examples:
  jsn flows executions "My Flow"
  jsn flows executions --limit 50
  jsn flows executions  # Interactive picker`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return runFlowsExecutions(cmd, name, limit)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of executions to show")

	return cmd
}

// runFlowsExecutions executes the flows executions command.
func runFlowsExecutions(cmd *cobra.Command, name string, limit int) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	outputWriter := appCtx.Output.(*output.Writer)
	sdkClient := appCtx.SDK.(*sdk.Client)

	// Interactive flow selection if no name provided
	if name == "" {
		isTerminal := output.IsTTY(cmd.OutOrStdout())
		if !isTerminal {
			return output.ErrUsage("Flow name is required in non-interactive mode")
		}

		selectedFlow, err := pickFlow(cmd.Context(), sdkClient, "Select a flow to view executions:")
		if err != nil {
			return err
		}
		name = selectedFlow
	}

	// Get the flow to get its sys_id
	flow, err := sdkClient.GetFlow(cmd.Context(), name)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}

	opts := &sdk.ListFlowExecutionsOptions{
		FlowID:    flow.SysID,
		Limit:     limit,
		OrderBy:   "sys_created_on",
		OrderDesc: true,
	}

	executions, err := sdkClient.ListFlowExecutions(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("failed to list flow executions: %w", err)
	}

	// Determine output format
	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledFlowExecutions(cmd, executions, flow.Name)
	}

	if format == output.FormatMarkdown {
		return printMarkdownFlowExecutions(cmd, executions, flow.Name)
	}

	// Build data for JSON
	var data []map[string]any
	for _, exec := range executions {
		data = append(data, map[string]any{
			"sys_id":         exec.SysID,
			"flow_id":        exec.FlowID,
			"flow_name":      exec.FlowName,
			"status":         exec.Status,
			"started":        exec.Started,
			"ended":          exec.Ended,
			"duration":       exec.Duration,
			"sys_updated_on": exec.SysUpdatedOn,
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d executions for flow '%s'", len(executions), flow.Name)),
		output.WithBreadcrumbs(
			output.Breadcrumb{
				Action:      "show",
				Cmd:         fmt.Sprintf("jsn flows %s", name),
				Description: "Show flow details",
			},
		),
	)
}

// printStyledFlowExecutions outputs styled flow executions list.
func printStyledFlowExecutions(cmd *cobra.Command, executions []sdk.FlowExecution, flowName string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00aa00"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#aa0000"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(fmt.Sprintf("Flow Executions (%s)", flowName)))
	fmt.Fprintln(cmd.OutOrStdout())

	if len(executions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), mutedStyle.Render("  No executions found for this flow."))
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
	}

	// Column headers
	fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %-12s %-20s %-10s %s\n",
		headerStyle.Render("Started"),
		headerStyle.Render("Duration"),
		headerStyle.Render("Status"),
		headerStyle.Render("Ended"),
		headerStyle.Render("Sys ID"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	// Executions
	for _, exec := range executions {
		statusStyle := mutedStyle
		switch exec.Status {
		case "success", "completed":
			statusStyle = successStyle
		case "error", "failed":
			statusStyle = errorStyle
		}

		started := exec.Started
		if started == "" {
			started = exec.SysUpdatedOn
		}
		if len(started) > 18 {
			started = started[:16]
		}

		duration := exec.Duration
		if duration == "" {
			duration = "-"
		}

		ended := exec.Ended
		if ended == "" {
			ended = "-"
		}
		if len(ended) > 10 {
			ended = ended[:10]
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %-12s %-20s %-10s %s\n",
			mutedStyle.Render(started),
			mutedStyle.Render(duration),
			statusStyle.Render(exec.Status),
			mutedStyle.Render(ended),
			mutedStyle.Render(exec.SysID),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownFlowExecutions outputs markdown flow executions list.
func printMarkdownFlowExecutions(cmd *cobra.Command, executions []sdk.FlowExecution, flowName string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "**Flow Executions (%s)**\n\n", flowName)

	if len(executions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No executions found for this flow.")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "| Started | Duration | Status | Ended | Sys ID |")
	fmt.Fprintln(cmd.OutOrStdout(), "|---------|----------|--------|-------|--------|")

	for _, exec := range executions {
		started := exec.Started
		if started == "" {
			started = exec.SysUpdatedOn
		}
		duration := exec.Duration
		if duration == "" {
			duration = "-"
		}
		ended := exec.Ended
		if ended == "" {
			ended = "-"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s | %s | %s |\n",
			started, duration, exec.Status, ended, exec.SysID)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledFlowInspection outputs comprehensive styled flow inspection.
func printStyledFlowInspection(cmd *cobra.Command, inspection *sdk.FlowInspection, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()
	triggerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00"))
	actionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA00"))
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00AAFF"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(fmt.Sprintf("Flow: %s", inspection.Flow.Name)))
	fmt.Fprintln(cmd.OutOrStdout())

	// Basic flow info
	status := "Inactive"
	if inspection.Flow.Active {
		status = "Active"
	}

	// Infer version if not set
	versionDisplay := inspection.Flow.Version
	if versionDisplay == "" {
		// Infer based on action instance types
		hasV1 := len(inspection.ActionInstances) > 0
		hasV2 := len(inspection.ActionInstancesV2) > 0
		if hasV2 && !hasV1 {
			versionDisplay = "Unset (Assumed V2)"
		} else if hasV1 && !hasV2 {
			versionDisplay = "Unset (Assumed V1)"
		} else if hasV1 && hasV2 {
			versionDisplay = "Unset (Mixed V1/V2)"
		} else {
			versionDisplay = "Unset"
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "  Status: %s | Version: %s\n", valueStyle.Render(status), mutedStyle.Render(versionDisplay))
	fmt.Fprintf(cmd.OutOrStdout(), "  Sys ID: %s\n", mutedStyle.Render(inspection.Flow.SysID))

	// Show link if available
	if instanceURL != "" {
		flowURL := fmt.Sprintf("%s/sys_hub_flow.do?sys_id=%s", instanceURL, inspection.Flow.SysID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Link: %s\n", linkStyle.Render(flowURL))
	}

	// Show Inputs/Outputs section only for subflows
	isSubflow := strings.EqualFold(inspection.Flow.Type, "subflow")
	if isSubflow {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), triggerStyle.Render("▶ SUBFLOW"))
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 50))

		// Show inputs
		if len(inspection.FlowInputs) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s (%d)\n", valueStyle.Render("Inputs"), len(inspection.FlowInputs))
			for _, input := range inspection.FlowInputs {
				label := getString(input, "label")
				name := getString(input, "name")
				inputType := getString(input, "type")
				mandatory := getString(input, "mandatory")

				displayName := label
				if displayName == "" {
					displayName = name
				}

				mandatoryStr := ""
				if mandatory == "true" {
					mandatoryStr = " (required)"
				}

				if inputType != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    • %s: %s%s\n", mutedStyle.Render(displayName), valueStyle.Render(inputType), mutedStyle.Render(mandatoryStr))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "    • %s%s\n", mutedStyle.Render(displayName), mutedStyle.Render(mandatoryStr))
				}
			}
		}

		// Show outputs
		if len(inspection.FlowOutputs) > 0 {
			if len(inspection.FlowInputs) > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %s (%d)\n", valueStyle.Render("Outputs"), len(inspection.FlowOutputs))
			for _, output := range inspection.FlowOutputs {
				label := getString(output, "label")
				name := getString(output, "name")
				outputType := getString(output, "type")

				displayName := label
				if displayName == "" {
					displayName = name
				}

				if outputType != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    • %s: %s\n", mutedStyle.Render(displayName), valueStyle.Render(outputType))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "    • %s\n", mutedStyle.Render(displayName))
				}
			}
		}

		// Add spacing before trigger section if there are also triggers
		if len(inspection.TriggerInstances) > 0 || len(inspection.Version) > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	// TRIGGER SECTION
	// Primary source: version payload's triggerInstances (has name, type, table)
	// Fallback: sys_hub_trigger_instance table (has flow field)
	triggerName := ""
	triggerType := ""
	triggerTable := ""
	triggerTime := ""

	if len(inspection.Version) > 0 {
		if tn, ok := inspection.Version["trigger_name"].(string); ok && tn != "" {
			triggerName = tn
		}
		if tt, ok := inspection.Version["trigger_type"].(string); ok && tt != "" {
			triggerType = tt
		}
		if tb, ok := inspection.Version["trigger_table"].(string); ok && tb != "" {
			triggerTable = tb
		}
		if tt, ok := inspection.Version["trigger_time"].(string); ok && tt != "" {
			// Extract just the time part (HH:MM:SS) from the datetime
			parts := strings.Split(tt, " ")
			if len(parts) == 2 {
				triggerTime = parts[1]
			} else {
				triggerTime = tt
			}
		}
	}

	// Fallback to trigger instances table
	if triggerName == "" && len(inspection.TriggerInstances) > 0 {
		ti := inspection.TriggerInstances[0]
		triggerName = getString(ti, "name")
		if triggerName == "" {
			triggerName = getString(ti, "trigger_type")
		}
		triggerType = getString(ti, "trigger_type")
	}

	if triggerName != "" || triggerType != "" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), triggerStyle.Render("▶ TRIGGER"))
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 50))

		// Display trigger name
		if triggerName != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", valueStyle.Render(triggerName))
		}

		// Display trigger type if different from name
		if triggerType != "" {
			// Format type for display (e.g., "record_create" -> "Record Create")
			typeDisplay := strings.ReplaceAll(triggerType, "_", " ")
			typeDisplay = titleCase(typeDisplay)
			if typeDisplay != triggerName {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", mutedStyle.Render("Type"), mutedStyle.Render(typeDisplay))
			}
		}

		// Display table for record-based triggers
		if triggerTable != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", mutedStyle.Render("Table"), valueStyle.Render(triggerTable))
		}

		// Display time for scheduled triggers
		if triggerTime != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", mutedStyle.Render("Time"), mutedStyle.Render(triggerTime))
		}

		// Display trigger condition from payload if available
		if len(inspection.Version) > 0 {
			if payload, ok := inspection.Version["payload"].(string); ok && payload != "" {
				var payloadData map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &payloadData); err == nil {
					if triggerInstances, ok := payloadData["triggerInstances"].([]interface{}); ok && len(triggerInstances) > 0 {
						if trigger, ok := triggerInstances[0].(map[string]interface{}); ok {
							if inputs, ok := trigger["inputs"].([]interface{}); ok {
								for _, input := range inputs {
									if inputMap, ok := input.(map[string]interface{}); ok {
										if name := getString(inputMap, "name"); name == "condition" {
											conditionValue := getString(inputMap, "value")
											if conditionValue != "" {
												// Format the condition for display
												formattedCondition := formatTriggerCondition(conditionValue)
												fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", mutedStyle.Render("Condition"), valueStyle.Render(formattedCondition))
											}
											break
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// ACTIONS SECTION
	// Build flow structure from version payload if available
	if len(inspection.Version) > 0 {
		if payload, ok := inspection.Version["payload"].(string); ok && payload != "" {
			var payloadData map[string]interface{}
			if err := json.Unmarshal([]byte(payload), &payloadData); err == nil {
				// Show flow structure with logic and actions
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), actionStyle.Render("⚡ FLOW STRUCTURE"))
				fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 50))

				// Collect all top-level items (actions, logic, subflows) sorted by order.
				// Logic instances with flowBlock arrays contain their children inline.
				// We build a tree by:
				//  1. Identifying which uiUniqueIdentifiers appear inside any flowBlock
				//  2. Top-level = items NOT inside any flowBlock
				//  3. Logic blocks' children come from their flowBlock array (recursive)

				// First pass: collect all uids that appear inside a flowBlock
				childUIDs := make(map[string]bool)
				var markChildren func(items []interface{})
				markChildren = func(items []interface{}) {
					for _, item := range items {
						if m, ok := item.(map[string]interface{}); ok {
							if uid, ok := m["uiUniqueIdentifier"].(string); ok && uid != "" {
								childUIDs[uid] = true
							}
							// Recurse into nested flowBlocks
							if fb, ok := m["flowBlock"].([]interface{}); ok {
								markChildren(fb)
							}
						}
					}
				}
				// Scan logic instances for flowBlock children
				if flowLogic, ok := payloadData["flowLogicInstances"].([]interface{}); ok {
					for _, logic := range flowLogic {
						if m, ok := logic.(map[string]interface{}); ok {
							if fb, ok := m["flowBlock"].([]interface{}); ok {
								markChildren(fb)
							}
						}
					}
				}

				// Build top-level step list (items not inside any flowBlock)
				var rootSteps []flowStep

				if actionInstances, ok := payloadData["actionInstances"].([]interface{}); ok {
					for _, action := range actionInstances {
						if m, ok := action.(map[string]interface{}); ok {
							uid := getString(m, "uiUniqueIdentifier")
							if uid != "" && childUIDs[uid] {
								continue // skip, it's a child of a logic block
							}
							orderStr := getString(m, "order")
							order, _ := strconv.Atoi(orderStr)
							rootSteps = append(rootSteps, flowStep{stepType: "action", data: m, order: order})
						}
					}
				}
				if subFlowInstances, ok := payloadData["subFlowInstances"].([]interface{}); ok {
					for _, sf := range subFlowInstances {
						if m, ok := sf.(map[string]interface{}); ok {
							uid := getString(m, "uiUniqueIdentifier")
							if uid != "" && childUIDs[uid] {
								continue
							}
							orderStr := getString(m, "order")
							order, _ := strconv.Atoi(orderStr)
							rootSteps = append(rootSteps, flowStep{stepType: "subflow", data: m, order: order})
						}
					}
				}
				if flowLogic, ok := payloadData["flowLogicInstances"].([]interface{}); ok {
					for _, logic := range flowLogic {
						if m, ok := logic.(map[string]interface{}); ok {
							uid := getString(m, "uiUniqueIdentifier")
							if uid != "" && childUIDs[uid] {
								continue
							}
							orderStr := getString(m, "order")
							order, _ := strconv.Atoi(orderStr)
							rootSteps = append(rootSteps, flowStep{stepType: "logic", data: m, order: order})
						}
					}
				}

				sort.Slice(rootSteps, func(i, j int) bool {
					return rootSteps[i].order < rootSteps[j].order
				})

				// Recursive walk: print a step, then if it's a logic block, walk its flowBlock children
				stepNum := 1
				var walkSteps func(steps []flowStep, indent int)
				walkSteps = func(steps []flowStep, indent int) {
					for _, step := range steps {
						printFlowStep(cmd, stepNum, indent, step, valueStyle, mutedStyle)
						stepNum++

						// If this is a logic block, walk its flowBlock children
						if step.stepType == "logic" {
							if fb, ok := step.data["flowBlock"].([]interface{}); ok && len(fb) > 0 {
								var children []flowStep
								for _, child := range fb {
									if m, ok := child.(map[string]interface{}); ok {
										childType := classifyPayloadItem(m)
										orderStr := getString(m, "order")
										order, _ := strconv.Atoi(orderStr)
										children = append(children, flowStep{stepType: childType, data: m, order: order})
									}
								}
								sort.Slice(children, func(i, j int) bool {
									return children[i].order < children[j].order
								})
								walkSteps(children, indent+1)
							}
						}
					}
				}
				walkSteps(rootSteps, 0)
			}
		}
	}

	// Fallback: show flat list from V1/V2 action instances + flow logic + subflow instances
	// when no version payload was available
	hasPayload := false
	if len(inspection.Version) > 0 {
		if payload, ok := inspection.Version["payload"].(string); ok && payload != "" {
			hasPayload = true
		}
	}
	if !hasPayload {
		type flatStep struct {
			order int
			label string
			name  string // original name (for subflow hints)
			kind  string // "action", "logic", "subflow"
		}
		var steps []flatStep

		// Add V1 action instances
		for _, action := range inspection.ActionInstances {
			name := ""
			if at, ok := action["action_type"].(map[string]interface{}); ok {
				name = getString(at, "display_value")
			}
			if name == "" {
				name = getString(action, "name")
			}
			if name == "" {
				name = getString(action, "display_text")
			}
			if name == "" {
				name = "Action"
			}
			orderStr := getString(action, "order")
			order, _ := strconv.Atoi(orderStr)
			steps = append(steps, flatStep{order: order, label: name, kind: "action"})
		}

		// Add V2 action instances
		for _, action := range inspection.ActionInstancesV2 {
			name := ""
			if at, ok := action["action_type"].(map[string]interface{}); ok {
				name = getString(at, "display_value")
			}
			if name == "" {
				name = getString(action, "name")
			}
			if name == "" {
				name = getString(action, "display_text")
			}
			if name == "" {
				name = "Action"
			}
			orderStr := getString(action, "order")
			order, _ := strconv.Atoi(orderStr)
			steps = append(steps, flatStep{order: order, label: name, kind: "action"})
		}

		// Add flow logic instances
		for _, logic := range inspection.FlowLogicInstances {
			name := getString(logic, "name")
			if name == "" {
				name = getString(logic, "display_text")
			}
			if name == "" {
				name = "Logic"
			}
			orderStr := getString(logic, "order")
			order, _ := strconv.Atoi(orderStr)
			steps = append(steps, flatStep{order: order, label: name, kind: "logic"})
		}

		// Add subflow instances
		for _, sf := range inspection.SubFlowInstances {
			name := getString(sf, "name")
			if name == "" {
				name = getString(sf, "display_text")
			}
			if name == "" {
				name = "Subflow"
			}
			orderStr := getString(sf, "order")
			order, _ := strconv.Atoi(orderStr)
			steps = append(steps, flatStep{order: order, label: "↪ " + name, name: name, kind: "subflow"})
		}

		if len(steps) > 0 {
			sort.Slice(steps, func(i, j int) bool {
				return steps[i].order < steps[j].order
			})

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), actionStyle.Render("⚡ FLOW STRUCTURE"))
			fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 50))
			for i, step := range steps {
				fmt.Fprintf(cmd.OutOrStdout(), "%d. %s\n", i+1, valueStyle.Render(step.label))
				if step.kind == "subflow" && step.name != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", mutedStyle.Render(fmt.Sprintf("jsn flows \"%s\"", step.name)))
				}
			}
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// flowStep represents an action, logic, or subflow step for tree display
type flowStep struct {
	stepType string // "action", "logic", or "subflow"
	data     map[string]interface{}
	order    int
}

// classifyPayloadItem determines the step type of a payload item by checking for
// type-specific fields: flowLogicDefinition (logic), subFlowType (subflow), else action.
func classifyPayloadItem(m map[string]interface{}) string {
	if _, ok := m["flowLogicDefinition"]; ok {
		return "logic"
	}
	if _, ok := m["subFlowType"]; ok {
		return "subflow"
	}
	// Some subflows lack subFlowType but have subflowSysId or subFlow
	if _, ok := m["subflowSysId"]; ok {
		return "subflow"
	}
	if _, ok := m["subFlow"]; ok {
		return "subflow"
	}
	return "action"
}

// printFlowStep prints a single flow step (action, logic, or subflow) with tree indentation.
// indent=0 is top-level, indent=1 adds 4 spaces, etc.
func printFlowStep(cmd *cobra.Command, stepNum int, indent int, step flowStep, valueStyle lipgloss.Style, mutedStyle lipgloss.Style) {
	pad := strings.Repeat("    ", indent)

	switch step.stepType {
	case "action":
		printActionStep(cmd, stepNum, pad, step.data, valueStyle, mutedStyle)
	case "subflow":
		printSubFlowStep(cmd, stepNum, pad, step.data, valueStyle, mutedStyle)
	default: // "logic"
		printLogicStep(cmd, stepNum, pad, step.data, valueStyle, mutedStyle)
	}
}

// printActionStep prints a flow action step with indentation
func printActionStep(cmd *cobra.Command, stepNum int, pad string, action map[string]interface{}, valueStyle lipgloss.Style, mutedStyle lipgloss.Style) {
	// Get action name from actionType.fName or fallbacks
	actionName := ""
	if actionType, ok := action["actionType"].(map[string]interface{}); ok {
		actionName = getString(actionType, "fName")
	}
	if actionName == "" {
		actionName = getString(action, "actionName")
	}
	if actionName == "" {
		actionName = getString(action, "actionInternalName")
	}
	if actionName == "" {
		actionName = getString(action, "name")
	}
	if actionName == "" {
		actionName = "Unknown Action"
	}

	// For Update Record actions, get the table name
	tableName := ""
	if inputs, ok := action["inputs"].([]interface{}); ok {
		for _, input := range inputs {
			if inputMap, ok := input.(map[string]interface{}); ok {
				if name := getString(inputMap, "name"); name == "table_name" {
					tableName = getString(inputMap, "displayValue")
					if tableName == "" {
						tableName = getString(inputMap, "value")
					}
					break
				}
			}
		}
	}

	// Strip flow name prefix from action names (e.g., "Software Procurement Flow : Add work notes" -> "Add work notes")
	if idx := strings.Index(actionName, " : "); idx > 0 {
		actionName = strings.TrimSpace(actionName[idx+3:])
	}

	// Build full action description
	actionDisplay := actionName
	if tableName != "" && actionName == "Update Record" {
		actionDisplay = actionName + " - " + tableName
	}

	// Get annotation/comment
	comment := getString(action, "comment")
	if comment == "" {
		comment = getString(action, "displayText")
	}

	// Print the action with indentation
	if comment != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s%d. %s (%s)\n", pad, stepNum, valueStyle.Render(actionDisplay), mutedStyle.Render(comment))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s%d. %s\n", pad, stepNum, valueStyle.Render(actionDisplay))
	}

	// Print input mappings if available (from V1 payload)
	if inputs, ok := action["inputs"].([]interface{}); ok && len(inputs) > 0 {
		for _, input := range inputs {
			if inputMap, ok := input.(map[string]interface{}); ok {
				inputName := getString(inputMap, "name")
				inputValue := getString(inputMap, "value")
				inputDisplay := getString(inputMap, "displayValue")

				// Skip empty values or template variables that aren't set
				if inputValue == "" && inputDisplay == "" {
					continue
				}

				// Get the label from parameter if available
				label := inputName
				if param, ok := inputMap["parameter"].(map[string]interface{}); ok {
					if paramLabel := getString(param, "label"); paramLabel != "" {
						label = paramLabel
					}
				}

				// Format the value for display
				displayValue := inputDisplay
				if displayValue == "" {
					displayValue = inputValue
				}

				// Truncate if too long
				if len(displayValue) > 50 {
					displayValue = displayValue[:47] + "..."
				}

				// Skip table_name as it's already shown in the action header
				if inputName == "table_name" {
					continue
				}

				// Print the input mapping with extra indentation
				fmt.Fprintf(cmd.OutOrStdout(), "%s    %s: %s\n", pad, mutedStyle.Render(label), valueStyle.Render(displayValue))
			}
		}
	}

	// Print input mappings from V2 values_decompressed if available
	if valuesDecompressed, ok := action["values_decompressed"].([]map[string]interface{}); ok && len(valuesDecompressed) > 0 {
		for _, inputMap := range valuesDecompressed {
			inputName := getString(inputMap, "name")
			inputValue := getString(inputMap, "value")
			inputDisplay := getString(inputMap, "displayValue")

			// Skip empty values
			if inputValue == "" && inputDisplay == "" {
				continue
			}

			// Get the label from parameter if available
			label := inputName
			if param, ok := inputMap["parameter"].(map[string]interface{}); ok {
				if paramLabel := getString(param, "label"); paramLabel != "" {
					label = paramLabel
				}
			}

			// Format the value for display
			displayValue := inputDisplay
			if displayValue == "" {
				displayValue = inputValue
			}

			// Truncate if too long
			if len(displayValue) > 50 {
				displayValue = displayValue[:47] + "..."
			}

			// Skip certain internal fields
			if inputName == "table_name" || inputName == "request_item_id" {
				continue
			}

			// Print the input mapping with extra indentation
			fmt.Fprintf(cmd.OutOrStdout(), "%s    %s: %s\n", pad, mutedStyle.Render(label), valueStyle.Render(displayValue))
		}
	}
}

// printSubFlowStep prints a subflow call step with indentation
func printSubFlowStep(cmd *cobra.Command, stepNum int, pad string, subFlow map[string]interface{}, valueStyle lipgloss.Style, mutedStyle lipgloss.Style) {
	// Get subflow name from subFlowType.fName or fallbacks
	subFlowName := ""
	if subFlowType, ok := subFlow["subFlowType"].(map[string]interface{}); ok {
		subFlowName = getString(subFlowType, "fName")
	}
	if subFlowName == "" {
		subFlowName = getString(subFlow, "subFlowName")
	}
	if subFlowName == "" {
		subFlowName = getString(subFlow, "subFlowInternalName")
	}
	// Check subFlow object (nested flow definition with name)
	if subFlowName == "" {
		if sf, ok := subFlow["subFlow"].(map[string]interface{}); ok {
			subFlowName = getString(sf, "name")
		}
	}
	if subFlowName == "" {
		subFlowName = getString(subFlow, "name")
	}
	if subFlowName == "" {
		subFlowName = "Unknown Subflow"
	}

	// Get annotation/comment
	comment := getString(subFlow, "comment")

	if comment != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s%d. %s (%s)\n", pad, stepNum, valueStyle.Render("↪ "+subFlowName), mutedStyle.Render(comment))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s%d. %s\n", pad, stepNum, valueStyle.Render("↪ "+subFlowName))
	}

	// Show drill-down hint
	fmt.Fprintf(cmd.OutOrStdout(), "%s   %s\n", pad, mutedStyle.Render(fmt.Sprintf("jsn flows \"%s\"", subFlowName)))
}

// printLogicStep prints a flow logic step with indentation
func printLogicStep(cmd *cobra.Command, stepNum int, pad string, logic map[string]interface{}, valueStyle lipgloss.Style, mutedStyle lipgloss.Style) {
	// Get logic type from flowLogicDefinition
	logicType := "Logic"
	if flowLogicDef, ok := logic["flowLogicDefinition"].(map[string]interface{}); ok {
		if name := getString(flowLogicDef, "name"); name != "" {
			logicType = name
		}
	}
	if logicType == "" {
		logicType = getString(logic, "name")
	}
	if logicType == "" {
		logicType = "Logic Step"
	}

	// Get annotation/comment
	comment := getString(logic, "comment")

	// Extract condition for If statements
	condition := ""
	conditionLabel := ""
	if logicType == "If" || logicType == "Else If" {
		if inputs, ok := logic["inputs"].([]interface{}); ok {
			for _, input := range inputs {
				if inputMap, ok := input.(map[string]interface{}); ok {
					inputName := getString(inputMap, "name")
					if inputName == "condition" {
						condition = getString(inputMap, "displayValue")
						if condition == "" {
							condition = getString(inputMap, "value")
						}
					} else if inputName == "condition_name" {
						conditionLabel = getString(inputMap, "displayValue")
						if conditionLabel == "" {
							conditionLabel = getString(inputMap, "value")
						}
					}
				}
			}
		}
	}

	// Build display text
	displayText := logicType
	if conditionLabel != "" {
		displayText = logicType + ": " + conditionLabel
	} else if condition != "" && len(condition) < 60 {
		displayText = logicType + ": " + condition
	}

	// Print the logic step with indentation
	fmt.Fprintf(cmd.OutOrStdout(), "%s%d. %s\n", pad, stepNum, valueStyle.Render(displayText))

	// Print condition on separate line if it's long
	if condition != "" && len(condition) >= 60 && conditionLabel == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s   %s: %s\n", pad, mutedStyle.Render("Condition"), valueStyle.Render(condition))
	}

	if comment != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s   %s: %s\n", pad, mutedStyle.Render("Annotation"), valueStyle.Render(comment))
	}

	// For Set Flow Variables, show the variables being set
	if logicType == "Set Flow Variables" {
		if flowVars, ok := logic["flowVariables"].([]interface{}); ok && len(flowVars) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "%s   %s:\n", pad, mutedStyle.Render("Variables Set"))
			for _, fv := range flowVars {
				if fvMap, ok := fv.(map[string]interface{}); ok {
					varName := getString(fvMap, "name")
					varValue := getString(fvMap, "displayValue")
					if varValue == "" {
						varValue = getString(fvMap, "value")
					}
					if varName != "" {
						if varValue != "" {
							fmt.Fprintf(cmd.OutOrStdout(), "%s     • %s = %s\n", pad, varName, valueStyle.Render(varValue))
						} else {
							fmt.Fprintf(cmd.OutOrStdout(), "%s     • %s\n", pad, varName)
						}
					}
				}
			}
		}
	}
}

// printMarkdownFlowInspection outputs comprehensive markdown flow inspection.
func printMarkdownFlowInspection(cmd *cobra.Command, inspection *sdk.FlowInspection) error {
	fmt.Fprintf(cmd.OutOrStdout(), "# Flow Inspection: %s\n\n", inspection.Flow.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "**Sys ID:** %s\n\n", inspection.Flow.SysID)
	fmt.Fprintf(cmd.OutOrStdout(), "**Active:** %v\n\n", inspection.Flow.Active)
	fmt.Fprintf(cmd.OutOrStdout(), "**Version:** %s\n\n", inspection.Flow.Version)

	// Combine components and actions into a single sorted list
	type flowItem struct {
		order    int
		itemType string
		name     string
		details  string
		comment  string
	}

	var items []flowItem

	// Build logic name map from payload if available
	logicNameMap := make(map[string]string)
	if len(inspection.Version) > 0 {
		if payload, ok := inspection.Version["payload"].(string); ok && payload != "" {
			var payloadData map[string]interface{}
			if err := json.Unmarshal([]byte(payload), &payloadData); err == nil {
				if flowLogic, ok := payloadData["flowLogicInstances"].([]interface{}); ok {
					for _, logic := range flowLogic {
						if logicMap, ok := logic.(map[string]interface{}); ok {
							// Get the uiUniqueIdentifier as the key
							uiID := ""
							if id, ok := logicMap["uiUniqueIdentifier"].(string); ok {
								uiID = id
							} else if id, ok := logicMap["id"].(string); ok {
								uiID = id
							}

							// Get the logic type name from flowLogicDefinition
							logicName := "Logic"
							if flowLogicDef, ok := logicMap["flowLogicDefinition"].(map[string]interface{}); ok {
								if name, ok := flowLogicDef["name"].(string); ok && name != "" {
									logicName = name
								}
							}
							if logicName == "" {
								if name, ok := logicMap["name"].(string); ok {
									logicName = name
								}
							}

							if uiID != "" {
								logicNameMap[uiID] = logicName
							}
						}
					}
				}
			}
		}
	}

	// Add components (excluding action/subflow instances - those are added separately with more detail)
	for _, comp := range inspection.Components {
		className := getString(comp, "sys_class_name")
		// Skip action instances and subflow instances - we'll add them separately
		if className == "sys_hub_action_instance" || className == "sys_hub_sub_flow_instance" {
			continue
		}

		orderStr := getString(comp, "order")
		order, _ := strconv.Atoi(orderStr)
		sysID := getString(comp, "sys_id")
		uiID := getString(comp, "ui_id")

		// Get the logic name from the payload data if available
		name := className
		if uiID != "" {
			if logicName, found := logicNameMap[uiID]; found {
				name = logicName
			}
		}

		items = append(items, flowItem{
			order:    order,
			itemType: className,
			name:     name,
			details:  sysID,
			comment:  "",
		})
	}

	// Add V1 action instances (may come from API or version payload)
	for _, action := range inspection.ActionInstances {
		orderStr := getString(action, "order")
		order, _ := strconv.Atoi(orderStr)

		// Handle both API format (action_type) and payload format (actionType.fName)
		actionName := getString(action, "action_type")
		if actionName == "" {
			if at, ok := action["actionType"].(map[string]interface{}); ok {
				actionName = getString(at, "fName")
			}
		}
		if actionName == "" {
			actionName = getString(action, "actionName")
		}

		comment := getString(action, "comment")
		sysID := getString(action, "sys_id")
		if sysID == "" {
			sysID = getString(action, "id")
		}

		items = append(items, flowItem{
			order:    order,
			itemType: "sys_hub_action_instance",
			name:     actionName,
			details:  sysID,
			comment:  comment,
		})
	}

	// Add subflow instances from payload (preferred — has names) or components (fallback)
	if len(inspection.SubFlowInstances) > 0 {
		for _, subFlow := range inspection.SubFlowInstances {
			orderStr := getString(subFlow, "order")
			order, _ := strconv.Atoi(orderStr)
			sysID := getString(subFlow, "id")

			subFlowName := ""
			if subFlowType, ok := subFlow["subFlowType"].(map[string]interface{}); ok {
				subFlowName = getString(subFlowType, "fName")
			}
			if subFlowName == "" {
				subFlowName = getString(subFlow, "subFlowName")
			}
			if subFlowName == "" {
				subFlowName = getString(subFlow, "name")
			}
			if subFlowName == "" {
				subFlowName = "Subflow"
			}

			comment := getString(subFlow, "comment")

			items = append(items, flowItem{
				order:    order,
				itemType: "sys_hub_sub_flow_instance",
				name:     subFlowName,
				details:  sysID,
				comment:  comment,
			})
		}
	} else {
		for _, comp := range inspection.Components {
			className := getString(comp, "sys_class_name")
			if className == "sys_hub_sub_flow_instance" {
				orderStr := getString(comp, "order")
				order, _ := strconv.Atoi(orderStr)
				sysID := getString(comp, "sys_id")

				items = append(items, flowItem{
					order:    order,
					itemType: "sys_hub_sub_flow_instance",
					name:     "Subflow",
					details:  sysID,
					comment:  "",
				})
			}
		}
	}

	// Add V2 action instances
	for _, action := range inspection.ActionInstancesV2 {
		orderStr := getString(action, "order")
		order, _ := strconv.Atoi(orderStr)
		actionType := getString(action, "action_type")
		sysID := getString(action, "sys_id")

		items = append(items, flowItem{
			order:    order,
			itemType: "sys_hub_action_instance_v2",
			name:     actionType,
			details:  sysID,
			comment:  "",
		})
	}

	// Sort by order
	sort.Slice(items, func(i, j int) bool {
		return items[i].order < items[j].order
	})

	// Print combined list
	if len(items) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "## Flow Steps (%d)\n\n", len(items))
		for _, item := range items {
			prefix := ""
			if item.itemType == "sys_hub_action_instance" {
				prefix = "[V1] "
			} else if item.itemType == "sys_hub_action_instance_v2" {
				prefix = "[V2] "
			} else if item.itemType == "sys_hub_sub_flow_instance" {
				prefix = "[Subflow] "
			}

			if item.comment != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "- Order %d: %s%s (%s)\n", item.order, prefix, item.name, item.comment)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "- Order %d: %s%s\n", item.order, prefix, item.name)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// Show Inputs/Outputs only for subflows
	isSubflow := strings.EqualFold(inspection.Flow.Type, "subflow")
	if isSubflow && len(inspection.FlowInputs) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "## Inputs (%d)\n\n", len(inspection.FlowInputs))
		for _, input := range inspection.FlowInputs {
			label := getString(input, "label")
			name := getString(input, "name")
			inputType := getString(input, "type")
			mandatory := getString(input, "mandatory")

			displayName := label
			if displayName == "" {
				displayName = name
			}

			mandatoryStr := ""
			if mandatory == "true" {
				mandatoryStr = " (required)"
			}

			if inputType != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "- **%s**: %s%s\n", displayName, inputType, mandatoryStr)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "- **%s**%s\n", displayName, mandatoryStr)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if isSubflow && len(inspection.FlowOutputs) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "## Outputs (%d)\n\n", len(inspection.FlowOutputs))
		for _, output := range inspection.FlowOutputs {
			label := getString(output, "label")
			name := getString(output, "name")
			outputType := getString(output, "type")

			displayName := label
			if displayName == "" {
				displayName = name
			}

			if outputType != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "- **%s**: %s\n", displayName, outputType)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "- **%s**\n", displayName)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// Show Trigger info in markdown from version payload or trigger instances
	triggerNameMD := ""
	triggerTypeMD := ""
	triggerTableMD := ""
	if len(inspection.Version) > 0 {
		if tn, ok := inspection.Version["trigger_name"].(string); ok && tn != "" {
			triggerNameMD = tn
		}
		if tt, ok := inspection.Version["trigger_type"].(string); ok && tt != "" {
			triggerTypeMD = tt
		}
		if tb, ok := inspection.Version["trigger_table"].(string); ok && tb != "" {
			triggerTableMD = tb
		}
	}
	if triggerNameMD == "" && len(inspection.TriggerInstances) > 0 {
		ti := inspection.TriggerInstances[0]
		triggerNameMD = getString(ti, "name")
		triggerTypeMD = getString(ti, "trigger_type")
	}
	if triggerNameMD != "" || triggerTypeMD != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "## Trigger\n\n")
		if triggerNameMD != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **%s**", triggerNameMD)
			if triggerTypeMD != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (%s)", triggerTypeMD)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "- Type: %s\n", triggerTypeMD)
		}
		if triggerTableMD != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "- Table: %s\n", triggerTableMD)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	return nil
}

// getString safely extracts a string value from a map.
// titleCase capitalizes the first letter of each space-separated word.
// Replaces deprecated strings.Title for simple cases.
// formatTriggerCondition formats an encoded ServiceNow condition query for display.
// It converts encoded queries like "sys_class_name=sn_vsc_login_event^ORsys_class_name=sn_vsc_role_granted_event"
// to human-readable format like "Event Type = Login Event OR Event Type = High Privilege Role Granted Event"
func formatTriggerCondition(condition string) string {
	if condition == "" {
		return ""
	}

	// Simple formatting: replace encoded operators with readable ones
	result := condition

	// Replace OR operator
	result = strings.ReplaceAll(result, "^OR", " OR ")

	// Replace AND operator
	result = strings.ReplaceAll(result, "^", " AND ")

	// Replace comparison operators
	result = strings.ReplaceAll(result, "=", " = ")
	result = strings.ReplaceAll(result, "!=", " != ")
	result = strings.ReplaceAll(result, ">=", " >= ")
	result = strings.ReplaceAll(result, "<=", " <= ")
	result = strings.ReplaceAll(result, ">", " > ")
	result = strings.ReplaceAll(result, "<", " < ")
	result = strings.ReplaceAll(result, "LIKE", " LIKE ")

	// Clean up field names (convert sys_class_name to Event Type for common patterns)
	if strings.Contains(result, "sys_class_name") {
		result = strings.ReplaceAll(result, "sys_class_name", "Event Type")
	}

	// Clean up multiple spaces
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}

	return strings.TrimSpace(result)
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case map[string]interface{}:
			if dv, ok := val["display_value"].(string); ok {
				return dv
			}
			if v, ok := val["value"].(string); ok {
				return v
			}
		}
	}
	return ""
}

// flowsCreateFlags holds the flags for the flows create command.
type flowsCreateFlags struct {
	name        string
	flowType    string
	description string
	active      bool
	runAs       string
	scope       string
	inputs      []string
	outputs     []string
	interactive bool
}

// newFlowsCreateCmd creates the flows create command.
func newFlowsCreateCmd() *cobra.Command {
	var flags flowsCreateFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new flow or subflow",
		Long: `Create a new Flow Designer flow or subflow.

Interactive Mode (default in TTY):
  When running in a terminal without required flags, you'll be prompted
  interactively to configure the flow step by step.

Non-Interactive Mode (scripts/CI):
  Use flags to specify all options. The --name flag is required.

Examples:
  # Interactive (TTY)
  jsn flows create

  # Non-interactive with flags
  jsn flows create --name "My Flow" --type flow
  jsn flows create --name "My Helper" --type subflow \
    --input "record_id:string:Record ID:true" \
    --output "result:boolean:Success"
  jsn flows create --name "System Flow" --active --run-as system

Input/Output Format:
  --input "name:type:label:required"
  --output "name:type:label"
  
  Types: string, integer, boolean, reference, choice, date, datetime
  Required: true or false (default: false)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlowsCreate(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.name, "name", "", "Flow name")
	cmd.Flags().StringVar(&flags.flowType, "type", "flow", "Flow type: flow or subflow")
	cmd.Flags().StringVar(&flags.description, "description", "", "Flow description")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Create as active")
	cmd.Flags().StringVar(&flags.runAs, "run-as", "user", "Run as: user or system")
	cmd.Flags().StringVar(&flags.scope, "scope", "", "Scope (defaults to current scope)")
	cmd.Flags().StringArrayVar(&flags.inputs, "input", nil, "Input variable (format: name:type:label:required)")
	cmd.Flags().StringArrayVar(&flags.outputs, "output", nil, "Output variable (format: name:type:label)")
	cmd.Flags().BoolVar(&flags.interactive, "interactive", false, "Force interactive mode (default auto-detected)")

	return cmd
}

// runFlowsCreate executes the flows create command.
func runFlowsCreate(cmd *cobra.Command, flags flowsCreateFlags) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	outputWriter := appCtx.Output.(*output.Writer)
	sdkClient := appCtx.SDK.(*sdk.Client)
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Determine if we should use interactive mode
	useInteractive := flags.interactive || (isTerminal && flags.name == "")

	// Interactive mode: prompt for missing values
	if useInteractive {
		if err := interactiveFlowCreate(cmd, sdkClient, &flags); err != nil {
			return err
		}
	}

	// Validate required fields
	if flags.name == "" {
		return output.ErrUsage("flow name is required (use --name or run interactively in a terminal)")
	}

	// Parse inputs
	var inputs []sdk.FlowVariableDef
	for _, inputStr := range flags.inputs {
		def, err := parseFlowVariableDef(inputStr, true)
		if err != nil {
			return output.ErrUsage(fmt.Sprintf("invalid input definition '%s': %v", inputStr, err))
		}
		inputs = append(inputs, def)
	}

	// Parse outputs
	var outputs []sdk.FlowVariableDef
	for _, outputStr := range flags.outputs {
		def, err := parseFlowVariableDef(outputStr, false)
		if err != nil {
			return output.ErrUsage(fmt.Sprintf("invalid output definition '%s': %v", outputStr, err))
		}
		outputs = append(outputs, def)
	}

	// Create flow or subflow based on type
	var flow *sdk.Flow
	var err error

	if flags.flowType == "subflow" {
		flow, err = sdkClient.CreateSubflow(cmd.Context(), sdk.CreateSubflowOptions{
			Name:        flags.name,
			Description: flags.description,
			Active:      flags.active,
			RunAs:       flags.runAs,
			Scope:       flags.scope,
			Inputs:      inputs,
			Outputs:     outputs,
		})
	} else {
		if len(inputs) > 0 || len(outputs) > 0 {
			return output.ErrUsage("inputs and outputs are only supported for subflows")
		}
		flow, err = sdkClient.CreateFlow(cmd.Context(), sdk.CreateFlowOptions{
			Name:        flags.name,
			Type:        flags.flowType,
			Description: flags.description,
			Active:      flags.active,
			RunAs:       flags.runAs,
			Scope:       flags.scope,
		})
	}

	if err != nil {
		return fmt.Errorf("failed to create flow: %w", err)
	}

	// Interactive mode: offer to add trigger and actions
	if useInteractive && flags.flowType != "subflow" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor).Render("Flow Created!"))

		addTrigger, _ := confirmPrompt("Would you like to add a trigger?")
		if addTrigger {
			if err := interactiveAddTrigger(cmd, sdkClient, flow.SysID); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: Could not add trigger: %v\n", err)
			}
		}
	}

	// Build summary
	flowTypeStr := "Flow"
	if flags.flowType == "subflow" {
		flowTypeStr = "Subflow"
	}

	data := map[string]any{
		"sys_id":      flow.SysID,
		"name":        flow.Name,
		"type":        flow.Type,
		"active":      flow.Active,
		"description": flow.Description,
	}

	if len(inputs) > 0 {
		data["inputs"] = len(inputs)
	}
	if len(outputs) > 0 {
		data["outputs"] = len(outputs)
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("Created %s '%s'", flowTypeStr, flow.Name)),
		output.WithBreadcrumbs(
			output.Breadcrumb{
				Action:      "show",
				Cmd:         fmt.Sprintf("jsn flows %s", flow.SysID),
				Description: "View flow details",
			},
			output.Breadcrumb{
				Action:      "list",
				Cmd:         "jsn flows",
				Description: "List all flows",
			},
		),
	)
}

// interactiveFlowCreate prompts the user for flow configuration interactively
func interactiveFlowCreate(cmd *cobra.Command, sdkClient *sdk.Client, flags *flowsCreateFlags) error {
	reader := bufio.NewReader(os.Stdin)

	// Flow name
	if flags.name == "" {
		fmt.Print("Flow name: ")
		name, _ := reader.ReadString('\n')
		flags.name = strings.TrimSpace(name)
	}

	// Flow type
	if flags.flowType == "flow" {
		items := []tui.PickerItem{
			{ID: "flow", Title: "Flow", Description: "Standard flow with trigger"},
			{ID: "subflow", Title: "Subflow", Description: "Reusable flow with inputs/outputs"},
		}
		selected, err := tui.Pick("Select flow type:", items)
		if err != nil || selected == nil {
			return fmt.Errorf("flow type selection cancelled")
		}
		flags.flowType = selected.ID
	}

	// Description
	if flags.description == "" {
		fmt.Print("Description (optional): ")
		desc, _ := reader.ReadString('\n')
		flags.description = strings.TrimSpace(desc)
	}

	// Run as
	if flags.runAs == "user" {
		items := []tui.PickerItem{
			{ID: "user", Title: "User", Description: "Run as the user who triggered the flow"},
			{ID: "system", Title: "System", Description: "Run with system privileges"},
		}
		selected, err := tui.Pick("Run as:", items)
		if err == nil && selected != nil {
			flags.runAs = selected.ID
		}
	}

	// Active
	if !flags.active {
		items := []tui.PickerItem{
			{ID: "draft", Title: "Draft (inactive)", Description: "Create as draft, activate later"},
			{ID: "active", Title: "Active", Description: "Activate immediately"},
		}
		selected, err := tui.Pick("Status:", items)
		if err == nil && selected != nil {
			flags.active = selected.ID == "active"
		}
	}

	// Subflow inputs/outputs
	if flags.flowType == "subflow" {
		addInputs, _ := confirmPrompt("Would you like to add input variables?")
		for addInputs {
			input, err := interactiveVariableDef(reader, "input")
			if err != nil {
				break
			}
			def := fmt.Sprintf("%s:%s:%s:%v", input.Name, input.Type, input.Label, input.Mandatory)
			flags.inputs = append(flags.inputs, def)
			addInputs, _ = confirmPrompt("Add another input?")
		}

		addOutputs, _ := confirmPrompt("Would you like to add output variables?")
		for addOutputs {
			output, err := interactiveVariableDef(reader, "output")
			if err != nil {
				break
			}
			def := fmt.Sprintf("%s:%s:%s", output.Name, output.Type, output.Label)
			flags.outputs = append(flags.outputs, def)
			addOutputs, _ = confirmPrompt("Add another output?")
		}
	}

	return nil
}

// interactiveVariableDef prompts for a variable definition
func interactiveVariableDef(reader *bufio.Reader, direction string) (sdk.FlowVariableDef, error) {
	var def sdk.FlowVariableDef

	fmt.Printf("%s variable name: ", strings.Title(direction))
	name, _ := reader.ReadString('\n')
	def.Name = strings.TrimSpace(name)
	if def.Name == "" {
		return def, fmt.Errorf("name is required")
	}

	// Type selection
	typeItems := []tui.PickerItem{
		{ID: "string", Title: "String", Description: "Text value"},
		{ID: "integer", Title: "Integer", Description: "Whole number"},
		{ID: "boolean", Title: "Boolean", Description: "True/False"},
		{ID: "reference", Title: "Reference", Description: "Reference to a table record"},
		{ID: "choice", Title: "Choice", Description: "Selection from options"},
		{ID: "date", Title: "Date", Description: "Date only"},
		{ID: "datetime", Title: "Date/Time", Description: "Date and time"},
	}
	selected, err := tui.Pick("Variable type:", typeItems)
	if err != nil || selected == nil {
		return def, fmt.Errorf("type selection cancelled")
	}
	def.Type = selected.ID

	// Reference table
	if def.Type == "reference" {
		fmt.Print("Reference table name (e.g., incident): ")
		ref, _ := reader.ReadString('\n')
		def.Reference = strings.TrimSpace(ref)
	}

	// Label
	fmt.Printf("Display label [%s]: ", def.Name)
	label, _ := reader.ReadString('\n')
	def.Label = strings.TrimSpace(label)
	if def.Label == "" {
		def.Label = def.Name
	}

	// Required (only for inputs)
	if direction == "input" {
		items := []tui.PickerItem{
			{ID: "optional", Title: "Optional", Description: "Not required"},
			{ID: "required", Title: "Required", Description: "Must be provided"},
		}
		selected, _ := tui.Pick("Is this required?", items)
		if selected != nil {
			def.Mandatory = selected.ID == "required"
		}
	}

	return def, nil
}

// confirmPrompt asks a yes/no question
func confirmPrompt(question string) (bool, error) {
	items := []tui.PickerItem{
		{ID: "yes", Title: "Yes", Description: ""},
		{ID: "no", Title: "No", Description: ""},
	}
	selected, err := tui.Pick(question, items, tui.WithAutoSelectSingle())
	if err != nil || selected == nil {
		return false, nil
	}
	return selected.ID == "yes", nil
}

// interactiveAddTrigger interactively configures a trigger for the flow
func interactiveAddTrigger(cmd *cobra.Command, sdkClient *sdk.Client, flowID string) error {
	// Trigger category selection
	categoryItems := []tui.PickerItem{
		{ID: "record", Title: "Record", Description: "Trigger when records change"},
		{ID: "scheduled", Title: "Scheduled", Description: "Run on a schedule"},
		{ID: "application", Title: "Application", Description: "Trigger from apps like Service Catalog"},
	}

	category, err := tui.Pick("Select trigger category:", categoryItems)
	if err != nil || category == nil {
		return nil
	}

	switch category.ID {
	case "record":
		return interactiveAddRecordTrigger(cmd, sdkClient, flowID)
	case "scheduled":
		return interactiveAddScheduledTrigger(cmd, sdkClient, flowID)
	case "application":
		return interactiveAddApplicationTrigger(cmd, sdkClient, flowID)
	}

	return nil
}

// interactiveAddRecordTrigger configures a record-based trigger
func interactiveAddRecordTrigger(cmd *cobra.Command, sdkClient *sdk.Client, flowID string) error {
	reader := bufio.NewReader(os.Stdin)

	// Trigger type
	typeItems := []tui.PickerItem{
		{ID: "create", Title: "Created", Description: "When a new record is created"},
		{ID: "update", Title: "Updated", Description: "When a record is updated"},
		{ID: "create_or_update", Title: "Created or Updated", Description: "When a record is created or updated"},
	}

	triggerType, err := tui.Pick("When should this trigger?", typeItems)
	if err != nil || triggerType == nil {
		return nil
	}

	// Table name
	fmt.Print("Table name (e.g., incident, change_request): ")
	table, _ := reader.ReadString('\n')
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("table name is required")
	}

	// Condition (optional)
	fmt.Print("Condition - when should it run? (optional, e.g., priority=1): ")
	condition, _ := reader.ReadString('\n')
	condition = strings.TrimSpace(condition)

	// Show what we're about to do
	fmt.Fprintf(cmd.OutOrStdout(), "\n")
	fmt.Fprintf(cmd.OutOrStdout(), lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor).Render("Creating trigger...")+"\n")

	// Create the trigger via SDK
	opts := sdk.CreateRecordTriggerOptions{
		FlowID:    flowID,
		Table:     table,
		Operation: triggerType.ID,
		Condition: condition,
	}

	if err := sdkClient.CreateRecordTrigger(cmd.Context(), opts); err != nil {
		return fmt.Errorf("failed to create trigger: %w", err)
	}

	// Success message
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00aa00"))
	fmt.Fprintf(cmd.OutOrStdout(), successStyle.Render("✓ Trigger created: %s on %s"), triggerType.Title, table)
	if condition != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " when %s", condition)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	return nil
}

// interactiveAddScheduledTrigger configures a scheduled trigger
func interactiveAddScheduledTrigger(cmd *cobra.Command, sdkClient *sdk.Client, flowID string) error {
	// Schedule type
	typeItems := []tui.PickerItem{
		{ID: "daily", Title: "Daily", Description: "Run every day"},
		{ID: "weekly", Title: "Weekly", Description: "Run on specific days of the week"},
		{ID: "monthly", Title: "Monthly", Description: "Run monthly"},
		{ID: "once", Title: "Run Once", Description: "Run one time at a specific date/time"},
		{ID: "repeat", Title: "Repeat", Description: "Repeat at intervals"},
	}

	scheduleType, err := tui.Pick("Schedule type:", typeItems)
	if err != nil || scheduleType == nil {
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	opts := sdk.CreateScheduledTriggerOptions{
		FlowID:   flowID,
		Schedule: scheduleType.ID,
	}

	// Time input (only daily, weekly, monthly need it)
	switch scheduleType.ID {
	case "daily", "weekly", "monthly":
		fmt.Fprint(cmd.OutOrStdout(), "  Time (HH:MM:SS, default 08:00:00): ")
		timeStr, _ := reader.ReadString('\n')
		timeStr = strings.TrimSpace(timeStr)
		if timeStr != "" {
			opts.Time = timeStr
		}
	}

	// Type-specific inputs
	switch scheduleType.ID {
	case "weekly":
		dayItems := []tui.PickerItem{
			{ID: "1", Title: "Monday"},
			{ID: "2", Title: "Tuesday"},
			{ID: "3", Title: "Wednesday"},
			{ID: "4", Title: "Thursday"},
			{ID: "5", Title: "Friday"},
			{ID: "6", Title: "Saturday"},
			{ID: "7", Title: "Sunday"},
		}
		day, err := tui.Pick("Day of week:", dayItems)
		if err != nil || day == nil {
			return nil
		}
		opts.Day = day.ID

	case "monthly":
		fmt.Fprint(cmd.OutOrStdout(), "  Day of month (1-31): ")
		dayStr, _ := reader.ReadString('\n')
		dayStr = strings.TrimSpace(dayStr)
		if dayStr == "" {
			dayStr = "1"
		}
		opts.Day = dayStr

	case "once":
		fmt.Fprint(cmd.OutOrStdout(), "  Date/time (YYYY-MM-DD HH:MM:SS): ")
		dateStr, _ := reader.ReadString('\n')
		dateStr = strings.TrimSpace(dateStr)
		if dateStr == "" {
			return fmt.Errorf("date/time is required for run-once triggers")
		}
		opts.Date = dateStr

	case "repeat":
		fmt.Fprintln(cmd.OutOrStdout(), "  Repeat interval:")
		fmt.Fprint(cmd.OutOrStdout(), "    Days (0-365, default 0): ")
		daysStr, _ := reader.ReadString('\n')
		daysStr = strings.TrimSpace(daysStr)
		days := 0
		if daysStr != "" {
			if d, err := strconv.Atoi(daysStr); err == nil {
				days = d
			}
		}

		fmt.Fprint(cmd.OutOrStdout(), "    Hours (0-23, default 0): ")
		hoursStr, _ := reader.ReadString('\n')
		hoursStr = strings.TrimSpace(hoursStr)
		hours := 0
		if hoursStr != "" {
			if h, err := strconv.Atoi(hoursStr); err == nil {
				hours = h
			}
		}

		fmt.Fprint(cmd.OutOrStdout(), "    Minutes (0-59, default 0): ")
		minsStr, _ := reader.ReadString('\n')
		minsStr = strings.TrimSpace(minsStr)
		mins := 0
		if minsStr != "" {
			if m, err := strconv.Atoi(minsStr); err == nil {
				mins = m
			}
		}

		fmt.Fprint(cmd.OutOrStdout(), "    Seconds (0-59, default 0): ")
		secsStr, _ := reader.ReadString('\n')
		secsStr = strings.TrimSpace(secsStr)
		secs := 0
		if secsStr != "" {
			if s, err := strconv.Atoi(secsStr); err == nil {
				secs = s
			}
		}

		if days == 0 && hours == 0 && mins == 0 && secs == 0 {
			return fmt.Errorf("repeat interval must be greater than zero")
		}

		// ServiceNow encodes repeat interval as duration-since-epoch datetime:
		// 1970-01-01 00:00:00 = 0, so add days to day 1, etc.
		opts.Repeat = fmt.Sprintf("1970-01-%02d %02d:%02d:%02d", days+1, hours, mins, secs)
	}

	err = sdkClient.CreateScheduledTrigger(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("failed to create scheduled trigger: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n  ✓ %s trigger added to flow\n", scheduleType.Title)

	return nil
}

// interactiveAddApplicationTrigger configures an application trigger
func interactiveAddApplicationTrigger(cmd *cobra.Command, sdkClient *sdk.Client, flowID string) error {
	// Application type
	typeItems := []tui.PickerItem{
		{ID: "service_catalog", Title: "Service Catalog", Description: "Trigger from catalog item"},
	}

	appType, err := tui.Pick("Application:", typeItems)
	if err != nil || appType == nil {
		return nil
	}

	err = sdkClient.CreateApplicationTrigger(cmd.Context(), sdk.CreateApplicationTriggerOptions{
		FlowID:      flowID,
		Application: appType.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to create application trigger: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n  ✓ %s trigger added to flow\n", appType.Title)

	return nil
}

// parseFlowVariableDef parses a variable definition string.
// Format for inputs: name:type:label:required
// Format for outputs: name:type:label
func parseFlowVariableDef(def string, isInput bool) (sdk.FlowVariableDef, error) {
	parts := strings.Split(def, ":")
	if len(parts) < 2 {
		return sdk.FlowVariableDef{}, fmt.Errorf("expected format: name:type[:label][:required]")
	}

	result := sdk.FlowVariableDef{
		Name: parts[0],
		Type: parts[1],
	}

	if len(parts) >= 3 {
		result.Label = parts[2]
	}
	if result.Label == "" {
		result.Label = result.Name
	}

	if isInput && len(parts) >= 4 {
		result.Mandatory = strings.ToLower(parts[3]) == "true"
	}

	return result, nil
}

// flowsAddTriggerFlags holds the flags for the flows add-trigger command.
type flowsAddTriggerFlags struct {
	triggerType string
	table       string
	condition   string
	schedule    string // daily, weekly, monthly, once, repeat
	time        string // HH:MM:SS for daily/weekly/monthly
	day         string // day of week (1-7) or day of month (1-31)
	date        string // YYYY-MM-DD HH:MM:SS for once
	repeat      string // duration for repeat (e.g., "1970-01-02 06:00:00")
}

// newFlowsAddTriggerCmd creates the flows add-trigger command.
func newFlowsAddTriggerCmd() *cobra.Command {
	var flags flowsAddTriggerFlags

	cmd := &cobra.Command{
		Use:   "add-trigger <flow_name_or_sys_id>",
		Short: "Add a trigger to an existing flow",
		Long: `Add a trigger to an existing flow.

Examples:
  # Interactive mode (in TTY)
  jsn flows add-trigger "My Flow"

  # Record triggers (non-interactive)
  jsn flows add-trigger "My Flow" --type record_create --table incident
  jsn flows add-trigger "My Flow" --type record_update --table ticket --condition "priority=1"

  # Scheduled triggers (non-interactive)
  jsn flows add-trigger "My Flow" --schedule daily --time 08:00:00
  jsn flows add-trigger "My Flow" --schedule weekly --day 3 --time 09:30:00
  jsn flows add-trigger "My Flow" --schedule monthly --day 15 --time 14:00:00
  jsn flows add-trigger "My Flow" --schedule once --date "2026-06-15 10:00:00"
  jsn flows add-trigger "My Flow" --schedule repeat --repeat "1970-01-02 06:00:00"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlowsAddTrigger(cmd, args[0], flags)
		},
	}

	cmd.Flags().StringVar(&flags.triggerType, "type", "", "Trigger type: record_create, record_update, record_create_or_update")
	cmd.Flags().StringVar(&flags.table, "table", "", "Table name (e.g., incident, change_request)")
	cmd.Flags().StringVar(&flags.condition, "condition", "", "Condition filter (optional, e.g., priority=1)")
	cmd.Flags().StringVar(&flags.schedule, "schedule", "", "Schedule type: daily, weekly, monthly, once, repeat")
	cmd.Flags().StringVar(&flags.time, "time", "", "Time to run (HH:MM:SS, default 08:00:00)")
	cmd.Flags().StringVar(&flags.day, "day", "", "Day of week (1-7, 1=Mon) or day of month (1-31)")
	cmd.Flags().StringVar(&flags.date, "date", "", "Date/time for run-once (YYYY-MM-DD HH:MM:SS)")
	cmd.Flags().StringVar(&flags.repeat, "repeat", "", "Repeat interval as duration (e.g., 1970-01-02 06:00:00 = 1 day)")

	return cmd
}

// runFlowsAddTrigger executes the flows add-trigger command.
func runFlowsAddTrigger(cmd *cobra.Command, flowID string, flags flowsAddTriggerFlags) error {
	appCtx := appctx.FromContext(cmd.Context())
	if appCtx == nil {
		return fmt.Errorf("app not initialized")
	}

	if appCtx.SDK == nil {
		return output.ErrAuth("no instance configured. Run: jsn setup")
	}

	sdkClient := appCtx.SDK.(*sdk.Client)
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Scheduled trigger path
	if flags.schedule != "" {
		opts := sdk.CreateScheduledTriggerOptions{
			FlowID:   flowID,
			Schedule: flags.schedule,
			Time:     flags.time,
			Day:      flags.day,
			Date:     flags.date,
			Repeat:   flags.repeat,
		}

		if err := sdkClient.CreateScheduledTrigger(cmd.Context(), opts); err != nil {
			return fmt.Errorf("failed to create scheduled trigger: %w", err)
		}

		outputWriter := appCtx.Output.(*output.Writer)
		return outputWriter.OK(map[string]interface{}{
			"flow":     flowID,
			"schedule": flags.schedule,
		}, output.WithSummary(fmt.Sprintf("Added %s trigger to flow", flags.schedule)))
	}

	// Record trigger path: if not all flags provided and in TTY, use interactive mode
	if isTerminal && (flags.triggerType == "" || flags.table == "") {
		return interactiveAddRecordTriggerToFlow(cmd, sdkClient, flowID)
	}

	// Validate required flags
	if flags.triggerType == "" {
		return output.ErrUsage("trigger type is required (use --type or --schedule, or run interactively)")
	}
	if flags.table == "" {
		return output.ErrUsage("table name is required (use --table or run interactively)")
	}

	// Map trigger type
	triggerType := flags.triggerType
	if triggerType == "create" {
		triggerType = "record_create"
	} else if triggerType == "update" {
		triggerType = "record_update"
	} else if triggerType == "create_or_update" {
		triggerType = "record_create_or_update"
	}

	// Create the trigger
	opts := sdk.CreateRecordTriggerOptions{
		FlowID:    flowID,
		Table:     flags.table,
		Operation: triggerType,
		Condition: flags.condition,
	}

	if err := sdkClient.CreateRecordTrigger(cmd.Context(), opts); err != nil {
		return fmt.Errorf("failed to create trigger: %w", err)
	}

	// Success output
	outputWriter := appCtx.Output.(*output.Writer)
	return outputWriter.OK(map[string]interface{}{
		"flow":      flowID,
		"trigger":   triggerType,
		"table":     flags.table,
		"condition": flags.condition,
	}, output.WithSummary(fmt.Sprintf("Added %s trigger on %s", triggerType, flags.table)))
}

// interactiveAddRecordTriggerToFlow interactively adds a trigger to an existing flow
func interactiveAddRecordTriggerToFlow(cmd *cobra.Command, sdkClient *sdk.Client, flowID string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor).Render("Add Trigger to Flow"))
	fmt.Println()

	// Trigger type
	typeItems := []tui.PickerItem{
		{ID: "record_create", Title: "Created", Description: "When a new record is created"},
		{ID: "record_update", Title: "Updated", Description: "When a record is updated"},
		{ID: "record_create_or_update", Title: "Created or Updated", Description: "When a record is created or updated"},
	}

	triggerType, err := tui.Pick("When should this trigger?", typeItems)
	if err != nil || triggerType == nil {
		return nil
	}

	// Table name
	fmt.Print("Table name (e.g., incident, change_request): ")
	table, _ := reader.ReadString('\n')
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("table name is required")
	}

	// Condition (optional)
	fmt.Print("Condition - when should it run? (optional, e.g., priority=1): ")
	condition, _ := reader.ReadString('\n')
	condition = strings.TrimSpace(condition)

	// Show what we're about to do
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor).Render("Creating trigger..."))

	// Create the trigger via SDK
	opts := sdk.CreateRecordTriggerOptions{
		FlowID:    flowID,
		Table:     table,
		Operation: triggerType.ID,
		Condition: condition,
	}

	if err := sdkClient.CreateRecordTrigger(cmd.Context(), opts); err != nil {
		return fmt.Errorf("failed to create trigger: %w", err)
	}

	// Success message
	fmt.Println()
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00aa00"))
	fmt.Printf(successStyle.Render("✓ Trigger created: %s on %s"), triggerType.Title, table)
	if condition != "" {
		fmt.Printf(" when %s", condition)
	}
	fmt.Println()
	fmt.Println()

	return nil
}
