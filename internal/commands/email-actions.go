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

// emailActionsFlags holds the flags for the email-actions command.
type emailActionsFlags struct {
	limit  int
	active bool
	search string
	query  string
	order  string
	desc   bool
	all    bool
}

// NewEmailActionsCmd creates the email-actions command.
func NewEmailActionsCmd() *cobra.Command {
	var flags emailActionsFlags

	cmd := &cobra.Command{
		Use:   "email-actions [<name_or_sys_id>]",
		Short: "Manage email notifications and inbound actions",
		Long: `List and inspect ServiceNow email notifications (sysevent_email_action) 
and inbound email actions (sys_email_action).

Usage:
  jsn email-actions <name_or_sys_id>                 Show action details
  jsn email-actions --search <term>                 Fuzzy search on name
  jsn email-actions --active                        Show only active actions

Examples:
  jsn email-actions "Incident Opened"
  jsn email-actions --search "approval" --active`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return runEmailActionsShow(cmd, args[0])
			}
			return runEmailActionsList(cmd, flags)
		},
	}

	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 20, "Maximum number of actions to fetch")
	cmd.Flags().BoolVar(&flags.active, "active", false, "Show only active actions")
	cmd.Flags().StringVar(&flags.search, "search", "", "Fuzzy search on name")
	cmd.Flags().StringVar(&flags.query, "query", "", "ServiceNow encoded query filter")
	cmd.Flags().StringVar(&flags.order, "order", "name", "Order by field")
	cmd.Flags().BoolVar(&flags.desc, "desc", false, "Sort in descending order")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Fetch all actions (no limit)")

	return cmd
}

// runEmailActionsList executes the email-actions list command.
func runEmailActionsList(cmd *cobra.Command, flags emailActionsFlags) error {
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
		queryParts = append(queryParts, wrapSimpleQuery(flags.query, "sysevent_email_action"))
	}
	sysparmQuery := strings.Join(queryParts, "^")

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	// Interactive mode
	useInteractive := isTerminal && !appCtx.NoInteractive() && format == output.FormatAuto
	if useInteractive {
		selected, err := pickEmailActionPaginated(cmd.Context(), sdkClient, sysparmQuery, flags.order, flags.desc)
		if err != nil {
			return err
		}
		if selected == "" {
			return fmt.Errorf("no action selected")
		}
		return runEmailActionsShow(cmd, selected)
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
		Fields:    []string{"sys_id", "name", "active", "table", "recipient_fields", "subject", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sysevent_email_action", opts)
	if err != nil {
		return fmt.Errorf("failed to list email actions: %w", err)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledEmailActions(cmd, records, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownEmailActions(cmd, records)
	}

	var data []map[string]any
	for _, r := range records {
		data = append(data, map[string]any{
			"sys_id":  getStringField(r, "sys_id"),
			"name":    getStringField(r, "name"),
			"active":  getFieldValue(r, "active"),
			"table":   getStringField(r, "table"),
			"subject": getStringField(r, "subject"),
			"scope":   getStringField(r, "sys_scope"),
		})
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("%d email actions", len(records))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "show", Cmd: "jsn email-actions <name>", Description: "Show action details"},
		),
	)
}

// runEmailActionsShow executes the email-actions show command.
func runEmailActionsShow(cmd *cobra.Command, name string) error {
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
		Fields: []string{"sys_id", "name", "active", "collection", "generation_type", "action_insert", "action_update", "event_name", "condition", "advanced_condition", "recipient_users", "recipient_fields", "recipient_groups", "template", "subject", "message_html", "message_text", "description", "sys_scope"},
	}

	records, err := sdkClient.ListRecords(cmd.Context(), "sysevent_email_action", opts)
	if err != nil {
		return fmt.Errorf("failed to get email action: %w", err)
	}

	if len(records) == 0 {
		return output.ErrNotFound(fmt.Sprintf("email action '%s' not found", name))
	}

	action := records[0]

	// Fetch template if referenced
	var template map[string]interface{}
	templateRef := getFieldValue(action, "template")
	if templateRef != nil {
		// Extract sys_id from reference field (either from value field or parse from link)
		var templateID string
		if refMap, ok := templateRef.(map[string]interface{}); ok {
			if val, ok := refMap["value"].(string); ok {
				templateID = val
			} else if link, ok := refMap["link"].(string); ok {
				// Parse sys_id from link: .../sysevent_email_template/50bf41d24a3623120191b782ab38061e
				parts := strings.Split(link, "/")
				if len(parts) > 0 {
					templateID = parts[len(parts)-1]
				}
			}
		}

		if templateID != "" {
			templateOpts := &sdk.ListRecordsOptions{
				Limit:  1,
				Query:  fmt.Sprintf("sys_id=%s", templateID),
				Fields: []string{"sys_id", "name", "subject", "message_html", "message_text"},
			}
			templateRecords, _ := sdkClient.ListRecords(cmd.Context(), "sysevent_email_template", templateOpts)
			if len(templateRecords) > 0 {
				template = templateRecords[0]
			}
		}
	}

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledEmailAction(cmd, action, template, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownEmailAction(cmd, action, template, instanceURL)
	}

	return outputWriter.OK(map[string]any{
		"action":   action,
		"template": template,
	},
		output.WithSummary(fmt.Sprintf("Email action: %s", getStringField(action, "name"))),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "list", Cmd: "jsn email-actions", Description: "List all actions"},
		),
	)
}

// pickEmailActionPaginated shows a paginated interactive picker for email actions.
func pickEmailActionPaginated(ctx context.Context, sdkClient *sdk.Client, query, orderBy string, orderDesc bool) (string, error) {
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
			Fields:    []string{"sys_id", "name", "active", "table", "subject"},
		}
		records, err := sdkClient.ListRecords(ctx, "sysevent_email_action", opts)
		if err != nil {
			return nil, err
		}

		var items []tui.PickerItem
		for _, r := range records {
			name := getStringField(r, "name")
			table := getStringField(r, "table")
			active := getFieldValue(r, "active")
			subject := getStringField(r, "subject")

			desc := table
			if subject != "" {
				desc += " | " + subject
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

	selected, err := tui.PickWithQueryablePagination("Select an email action:", fetcher, tui.WithMaxVisible(15))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", nil
	}
	return selected.ID, nil
}

// printStyledEmailActions outputs styled email actions list.
func printStyledEmailActions(cmd *cobra.Command, actions []map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Email Actions"))
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-20s %s\n",
		headerStyle.Render("Name"),
		headerStyle.Render("Table"),
		headerStyle.Render("Subject"),
	)
	fmt.Fprintln(cmd.OutOrStdout())

	for _, a := range actions {
		name := getStringField(a, "name")
		table := getStringField(a, "table")
		subject := getStringField(a, "subject")
		active := getFieldValue(a, "active")

		if active == false || active == "false" {
			name = "[inactive] " + name
		}

		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(subject) > 35 {
			subject = subject[:32] + "..."
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %-20s %s\n",
			name,
			mutedStyle.Render(table),
			mutedStyle.Render(subject),
		)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn email-actions <name>",
		mutedStyle.Render("Show action details"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownEmailActions outputs markdown email actions list.
func printMarkdownEmailActions(cmd *cobra.Command, actions []map[string]interface{}) error {
	fmt.Fprintln(cmd.OutOrStdout(), "**Email Actions**")
	fmt.Fprintln(cmd.OutOrStdout(), "| Name | Table | Subject |")
	fmt.Fprintln(cmd.OutOrStdout(), "|------|-------|---------|")

	for _, a := range actions {
		name := getStringField(a, "name")
		table := getStringField(a, "table")
		subject := getStringField(a, "subject")

		fmt.Fprintf(cmd.OutOrStdout(), "| %s | %s | %s |\n", name, table, subject)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printStyledEmailAction outputs styled email action details.
func printStyledEmailAction(cmd *cobra.Command, action, template map[string]interface{}, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	valueStyle := lipgloss.NewStyle()
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(getStringField(action, "name")))
	fmt.Fprintln(cmd.OutOrStdout())

	// Add link to record
	if instanceURL != "" {
		sysID := getStringField(action, "sys_id")
		link := fmt.Sprintf("%s/sysevent_email_action.do?sys_id=%s", instanceURL, sysID)
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Link:"),
			fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", link, mutedStyle.Render("Open in Instance")),
		)
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// Collection (Table)
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Collection (Table)"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
		labelStyle.Render("Table:"),
		valueStyle.Render(getStringField(action, "collection")))
	fmt.Fprintln(cmd.OutOrStdout())

	// When to Send Section
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("When to Send"))
	generationType := getStringField(action, "generation_type")
	fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
		labelStyle.Render("Send When:"),
		valueStyle.Render(generationType))

	if generationType == "engine" {
		actionInsert := getFieldValue(action, "action_insert")
		actionUpdate := getFieldValue(action, "action_update")
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Inserted:"),
			valueStyle.Render(fmt.Sprintf("%v", actionInsert)))
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Updated:"),
			valueStyle.Render(fmt.Sprintf("%v", actionUpdate)))

		condition := getStringField(action, "condition")
		if condition != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render("Conditions:"),
				valueStyle.Render(condition))
		}

		advancedCondition := getStringField(action, "advanced_condition")
		if advancedCondition != "" {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), labelStyle.Render("  Advanced Condition:"))
			lines := strings.Split(advancedCondition, "\n")
			for i, line := range lines {
				if i >= 5 {
					fmt.Fprintln(cmd.OutOrStdout(), "    "+mutedStyle.Render("..."))
					break
				}
				if len(line) > 70 {
					line = line[:67] + "..."
				}
				fmt.Fprintln(cmd.OutOrStdout(), "    "+valueStyle.Render(line))
			}
		}
	}

	// Show event name if present (for both engine and event types)
	eventName := getStringField(action, "event_name")
	if eventName != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Event Name:"),
			valueStyle.Render(eventName))
	}
	fmt.Fprintln(cmd.OutOrStdout())

	// Who Will Receive Section
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Who Will Receive"))
	recipientUsers := getStringField(action, "recipient_users")
	recipientFields := getStringField(action, "recipient_fields")
	recipientGroups := getStringField(action, "recipient_groups")

	if recipientUsers != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Users:"),
			valueStyle.Render(recipientUsers))
	}
	if recipientFields != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("User/Group Fields:"),
			valueStyle.Render(recipientFields))
	}
	if recipientGroups != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Groups:"),
			valueStyle.Render(recipientGroups))
	}
	fmt.Fprintln(cmd.OutOrStdout())

	// What It Will Contain Section
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("What It Will Contain"))

	templateID := getStringField(action, "template")
	if templateID != "" && template != nil {
		templateName := getStringField(template, "name")
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Template:"),
			valueStyle.Render(templateName))

		// Show template subject if action doesn't have one
		templateSubject := getStringField(template, "subject")
		if templateSubject != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
				labelStyle.Render("Subject (from template):"),
				valueStyle.Render(templateSubject))
		}
	}

	subject := getStringField(action, "subject")
	if subject != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s  %s\n",
			labelStyle.Render("Subject:"),
			valueStyle.Render(subject))
	}

	// Show message from action or template
	messageHTML := getStringField(action, "message_html")
	if messageHTML == "" && template != nil {
		messageHTML = getStringField(template, "message_html")
	}
	if messageHTML != "" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), labelStyle.Render("  Message HTML:"))
		lines := strings.Split(messageHTML, "\n")
		for i, line := range lines {
			if i >= 10 {
				fmt.Fprintln(cmd.OutOrStdout(), "    "+mutedStyle.Render("..."))
				break
			}
			if len(line) > 70 {
				line = line[:67] + "..."
			}
			fmt.Fprintln(cmd.OutOrStdout(), "    "+valueStyle.Render(line))
		}
	}

	messageText := getStringField(action, "message_text")
	if messageText == "" && template != nil {
		messageText = getStringField(template, "message_text")
	}
	if messageText != "" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), labelStyle.Render("  Message Text:"))
		lines := strings.Split(messageText, "\n")
		for i, line := range lines {
			if i >= 10 {
				fmt.Fprintln(cmd.OutOrStdout(), "    "+mutedStyle.Render("..."))
				break
			}
			if len(line) > 70 {
				line = line[:67] + "..."
			}
			fmt.Fprintln(cmd.OutOrStdout(), "    "+valueStyle.Render(line))
		}
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "─────")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn email-actions",
		labelStyle.Render("List all actions"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// printMarkdownEmailAction outputs markdown email action details.
func printMarkdownEmailAction(cmd *cobra.Command, action, template map[string]interface{}, instanceURL string) error {
	name := getStringField(action, "name")
	sysID := getStringField(action, "sys_id")

	// Add link to record
	if instanceURL != "" {
		link := fmt.Sprintf("%s/sysevent_email_action.do?sys_id=%s", instanceURL, sysID)
		fmt.Fprintf(cmd.OutOrStdout(), "**[%s](%s)**\n\n", name, link)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", name)
	}

	// Collection (Table)
	fmt.Fprintln(cmd.OutOrStdout(), "#### Collection (Table)")
	fmt.Fprintf(cmd.OutOrStdout(), "- **Table:** %s\n", getStringField(action, "collection"))
	fmt.Fprintln(cmd.OutOrStdout())

	// When to Send Section
	fmt.Fprintln(cmd.OutOrStdout(), "#### When to Send")
	generationType := getStringField(action, "generation_type")
	fmt.Fprintf(cmd.OutOrStdout(), "- **Send When:** %s\n", generationType)

	if generationType == "engine" {
		actionInsert := getFieldValue(action, "action_insert")
		actionUpdate := getFieldValue(action, "action_update")
		fmt.Fprintf(cmd.OutOrStdout(), "- **Inserted:** %v\n", actionInsert)
		fmt.Fprintf(cmd.OutOrStdout(), "- **Updated:** %v\n", actionUpdate)

		condition := getStringField(action, "condition")
		if condition != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **Conditions:** `%s`\n", condition)
		}

		advancedCondition := getStringField(action, "advanced_condition")
		if advancedCondition != "" {
			fmt.Fprintln(cmd.OutOrStdout(), "- **Advanced Condition:**")
			fmt.Fprintln(cmd.OutOrStdout(), "```javascript")
			fmt.Fprintln(cmd.OutOrStdout(), advancedCondition)
			fmt.Fprintln(cmd.OutOrStdout(), "```")
		}
	}

	// Show event name if present (for both engine and event types)
	eventName := getStringField(action, "event_name")
	if eventName != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "- **Event Name:** %s\n", eventName)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	// Who Will Receive Section
	fmt.Fprintln(cmd.OutOrStdout(), "#### Who Will Receive")
	recipientUsers := getStringField(action, "recipient_users")
	recipientFields := getStringField(action, "recipient_fields")
	recipientGroups := getStringField(action, "recipient_groups")

	if recipientUsers != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "- **Users:** %s\n", recipientUsers)
	}
	if recipientFields != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "- **User/Group Fields:** %s\n", recipientFields)
	}
	if recipientGroups != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "- **Groups:** %s\n", recipientGroups)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	// What It Will Contain Section
	fmt.Fprintln(cmd.OutOrStdout(), "#### What It Will Contain")

	templateID := getStringField(action, "template")
	if templateID != "" && template != nil {
		templateName := getStringField(template, "name")
		fmt.Fprintf(cmd.OutOrStdout(), "- **Template:** %s\n", templateName)

		// Show template subject if action doesn't have one
		templateSubject := getStringField(template, "subject")
		if templateSubject != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "- **Subject (from template):** %s\n", templateSubject)
		}
	}

	subject := getStringField(action, "subject")
	if subject != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "- **Subject:** %s\n", subject)
	}

	// Show message from action or template
	messageHTML := getStringField(action, "message_html")
	if messageHTML == "" && template != nil {
		messageHTML = getStringField(template, "message_html")
	}
	if messageHTML != "" {
		fmt.Fprintln(cmd.OutOrStdout(), "- **Message HTML:**")
		fmt.Fprintln(cmd.OutOrStdout(), "```html")
		fmt.Fprintln(cmd.OutOrStdout(), messageHTML)
		fmt.Fprintln(cmd.OutOrStdout(), "```")
	}

	messageText := getStringField(action, "message_text")
	if messageText == "" && template != nil {
		messageText = getStringField(template, "message_text")
	}
	if messageText != "" {
		fmt.Fprintln(cmd.OutOrStdout(), "- **Message Text:**")
		fmt.Fprintln(cmd.OutOrStdout(), "```")
		fmt.Fprintln(cmd.OutOrStdout(), messageText)
		fmt.Fprintln(cmd.OutOrStdout(), "```")
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
