package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jacebenson/jsn/internal/appctx"
	"github.com/jacebenson/jsn/internal/auth"
	"github.com/jacebenson/jsn/internal/config"
	"github.com/jacebenson/jsn/internal/output"
	"github.com/jacebenson/jsn/internal/sdk"
	"github.com/spf13/cobra"
)

// codeSearchFlags holds the flags for the code-search command.
type codeSearchFlags struct {
	term        string
	table       string
	scope       string
	searchGroup string
	limit       int
}

// CodeSearchHit represents a single match within a record
type CodeSearchHit struct {
	SysID     string `json:"sysId"`
	Name      string `json:"name"`
	ClassName string `json:"className"`
	Matches   []struct {
		Field       string `json:"field"`
		FieldLabel  string `json:"fieldLabel"`
		LineMatches []struct {
			Line    float64 `json:"line"`
			Context string  `json:"context"`
			Escaped string  `json:"escaped"`
		} `json:"lineMatches"`
	} `json:"matches"`
}

// CodeSearchTableResult represents results for one table
type CodeSearchTableResult struct {
	RecordType string          `json:"recordType"`
	TableLabel string          `json:"tableLabel"`
	Hits       []CodeSearchHit `json:"hits"`
}

// CodeSearchResponse represents the API response
type CodeSearchResponse struct {
	Result []CodeSearchTableResult `json:"result"`
}

// NewCodeSearchCmd creates the code-search command.
func NewCodeSearchCmd() *cobra.Command {
	var flags codeSearchFlags

	cmd := &cobra.Command{
		Use:   "code-search <term>",
		Short: "Search code across ServiceNow script tables",
		Long: `Search for code patterns across ServiceNow script tables using the sn_codesearch API.

This command searches through business rules, script includes, client scripts, 
UI policies, and other scriptable tables for the given term.

Usage:
  jsn code-search "GlideRecord"                    Search for GlideRecord usage
  jsn code-search "getCaller" --table sys_script    Search only in business rules
  jsn code-search "myFunction" --scope x_my_app    Search in specific scope

Supported Tables:
  - sys_script (Business Rules)
  - sys_script_include (Script Includes)
  - sys_ui_script (UI Scripts)
  - sys_client_script (Client Scripts)
  - sys_ui_policy (UI Policies)
  - sys_ws_operation (Scripted REST Operations)
  - sysauto_script (Scheduled Jobs)
  - sysevent_email_action (Email Scripts)
  - wf_activity (Workflow Activities)
  - sys_variable_value (Workflow Variables)

Note: This requires the sn_codesearch application to be installed on your instance.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.term = args[0]
			return runCodeSearch(cmd, flags)
		},
	}

	cmd.Flags().StringVarP(&flags.table, "table", "t", "", "Search only in specific table (e.g., sys_script)")
	cmd.Flags().StringVarP(&flags.scope, "scope", "s", "", "Filter by application scope")
	cmd.Flags().StringVar(&flags.searchGroup, "search-group", "x_8821_code.default", "Code search group to use")
	cmd.Flags().IntVarP(&flags.limit, "limit", "n", 50, "Maximum number of results to show")

	return cmd
}

// runCodeSearch executes the code search.
func runCodeSearch(cmd *cobra.Command, flags codeSearchFlags) error {
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

	_ = appCtx.SDK.(*sdk.Client) // Ensure SDK is initialized

	// Build the API query
	query := url.Values{}
	query.Set("search_group", flags.searchGroup)
	query.Set("term", flags.term)
	query.Set("search_all_scopes", "true")
	if flags.table != "" {
		query.Set("table", flags.table)
	}

	// Make the API call using raw REST endpoint
	endpoint := fmt.Sprintf("%s/api/sn_codesearch/code_search/search?%s", profile.InstanceURL, query.Encode())

	// Create HTTP request
	req, err := http.NewRequestWithContext(cmd.Context(), "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// Get credentials from auth manager
	authManager := auth.NewManager(cfg)
	creds, err := authManager.GetCredentials()
	if err != nil {
		return output.ErrAuth("failed to get credentials: " + err.Error())
	}

	// Set authentication
	if profile.AuthMethod == "gck" {
		req.Header.Set("X-UserToken", creds.Token)
		if creds.Cookies != "" {
			req.Header.Set("Cookie", creds.Cookies)
		}
	} else {
		req.SetBasicAuth(creds.Username, creds.Token)
	}

	// Execute request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("code search failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return output.ErrUsage("The sn_codesearch application is not installed on this instance.\n" +
			"Code search requires the 'Code Search' scoped application.\n" +
			"You can install it from the ServiceNow Store or use 'jsn rest' as an alternative.")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var searchResp CodeSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	format := outputWriter.GetFormat()
	isTerminal := output.IsTTY(cmd.OutOrStdout())

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledCodeSearch(cmd, searchResp.Result, flags.term, instanceURL)
	}

	if format == output.FormatStyled || (format == output.FormatAuto && isTerminal) {
		return printStyledCodeSearch(cmd, searchResp.Result, flags.term, instanceURL)
	}

	if format == output.FormatMarkdown {
		return printMarkdownCodeSearch(cmd, searchResp.Result, flags.term, instanceURL)
	}

	// Build data for JSON output
	var data []map[string]any
	matchCount := 0
	for _, tableResult := range searchResp.Result {
		for _, hit := range tableResult.Hits {
			for _, match := range hit.Matches {
				for _, lineMatch := range match.LineMatches {
					data = append(data, map[string]any{
						"table":       tableResult.RecordType,
						"table_label": tableResult.TableLabel,
						"record_id":   hit.SysID,
						"record_name": hit.Name,
						"field":       match.Field,
						"line":        int(lineMatch.Line),
						"context":     lineMatch.Context,
						"link":        fmt.Sprintf("%s/%s.do?sys_id=%s", instanceURL, hit.ClassName, hit.SysID),
					})
					matchCount++
					if matchCount >= flags.limit {
						break
					}
				}
				if matchCount >= flags.limit {
					break
				}
			}
			if matchCount >= flags.limit {
				break
			}
		}
		if matchCount >= flags.limit {
			break
		}
	}

	return outputWriter.OK(data,
		output.WithSummary(fmt.Sprintf("Found %d matches for '%s'", len(data), flags.term)),
		output.WithBreadcrumbs(
			output.Breadcrumb{Action: "search", Cmd: "jsn code-search <term>", Description: "Search for different term"},
			output.Breadcrumb{Action: "filter", Cmd: "jsn code-search <term> --table <table>", Description: "Filter by table"},
		),
	)
}

// printStyledCodeSearch outputs styled code search results.
func printStyledCodeSearch(cmd *cobra.Command, results []CodeSearchTableResult, term, instanceURL string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(output.BrandColor)
	tableStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#666666"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	lineNumStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Width(4).Align(lipgloss.Right)
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc"))
	matchStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e8a217")) // Brand color for matches
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4a9eff"))             // Blue for file/record names

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render(fmt.Sprintf("Code Search: %s", term)))
	fmt.Fprintln(cmd.OutOrStdout())

	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), mutedStyle.Render("No matches found."))
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
	}

	totalMatches := 0
	totalRecords := 0
	for _, tableResult := range results {
		totalRecords += len(tableResult.Hits)
		for _, hit := range tableResult.Hits {
			totalMatches += len(hit.Matches)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Found %s in %s\n\n",
		headerStyle.Render(fmt.Sprintf("%d matches", totalMatches)),
		mutedStyle.Render(fmt.Sprintf("%d records", totalRecords)),
	)

	for _, tableResult := range results {
		if len(tableResult.Hits) == 0 {
			continue
		}

		label := tableResult.TableLabel
		if label == "" {
			label = tableResult.RecordType
		}
		fmt.Fprintln(cmd.OutOrStdout(), tableStyle.Render(fmt.Sprintf("▸ %s [%s]", label, tableResult.RecordType)))

		for _, hit := range tableResult.Hits {
			// Record name as a file-like header
			recordName := hit.Name
			if len(recordName) > 50 {
				recordName = recordName[:47] + "..."
			}

			if instanceURL != "" {
				link := fmt.Sprintf("%s/%s.do?sys_id=%s", instanceURL, hit.ClassName, hit.SysID)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s %s\n",
					mutedStyle.Render("→"),
					fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", link, fileStyle.Render(recordName)),
				)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s %s\n",
					mutedStyle.Render("→"),
					fileStyle.Render(recordName),
				)
			}

			// Show matches - group by field
			for _, match := range hit.Matches {
				// Show field name once
				fmt.Fprintf(cmd.OutOrStdout(), "    %s %s\n",
					mutedStyle.Render("│"),
					mutedStyle.Render(match.Field),
				)

				// Show code lines with better formatting
				for i, lineMatch := range match.LineMatches {
					context := lineMatch.Context
					// Truncate long lines
					if len(context) > 80 {
						context = context[:77] + "..."
					}

					// Highlight the search term (case insensitive)
					highlighted := highlightTerm(context, term, matchStyle)

					// Use different connector for last line
					connector := "│"
					if i == len(match.LineMatches)-1 {
						connector = "└"
					}

					fmt.Fprintf(cmd.OutOrStdout(), "    %s %s %s\n",
						mutedStyle.Render(connector),
						lineNumStyle.Render(fmt.Sprintf("%d", int(lineMatch.Line))),
						codeStyle.Render(highlighted),
					)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 60))
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), headerStyle.Render("Hints:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn code-search <term> --table sys_script",
		mutedStyle.Render("Search only business rules"),
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %-50s  %s\n",
		"jsn records --table <table> <sys_id>",
		mutedStyle.Render("View full record"),
	)

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// highlightTerm highlights all occurrences of term in text (case insensitive)
func highlightTerm(text, term string, highlightStyle lipgloss.Style) string {
	if term == "" {
		return text
	}

	// Case insensitive replace
	lowerText := strings.ToLower(text)
	lowerTerm := strings.ToLower(term)

	var result strings.Builder
	start := 0
	for {
		idx := strings.Index(lowerText[start:], lowerTerm)
		if idx == -1 {
			result.WriteString(text[start:])
			break
		}
		idx += start

		// Write text before match
		result.WriteString(text[start:idx])

		// Write highlighted match
		result.WriteString(highlightStyle.Render(text[idx : idx+len(term)]))

		start = idx + len(term)
	}

	return result.String()
}

// printMarkdownCodeSearch outputs markdown code search results.
func printMarkdownCodeSearch(cmd *cobra.Command, results []CodeSearchTableResult, term, instanceURL string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "## Code Search: `%s`\n\n", term)

	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "*No matches found.*")
		return nil
	}

	totalMatches := 0
	totalRecords := 0
	for _, tableResult := range results {
		totalRecords += len(tableResult.Hits)
		for _, hit := range tableResult.Hits {
			totalMatches += len(hit.Matches)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Found **%d matches** in **%d records**\n\n", totalMatches, totalRecords)

	for _, tableResult := range results {
		if len(tableResult.Hits) == 0 {
			continue
		}

		label := tableResult.TableLabel
		if label == "" {
			label = tableResult.RecordType
		}
		fmt.Fprintf(cmd.OutOrStdout(), "### %s (`%s`)\n\n", label, tableResult.RecordType)

		for _, hit := range tableResult.Hits {
			recordName := hit.Name
			if len(recordName) > 60 {
				recordName = recordName[:57] + "..."
			}

			if instanceURL != "" {
				link := fmt.Sprintf("%s/%s.do?sys_id=%s", instanceURL, hit.ClassName, hit.SysID)
				fmt.Fprintf(cmd.OutOrStdout(), "**[%s](%s)**\n\n", recordName, link)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "**%s**\n\n", recordName)
			}

			for _, match := range hit.Matches {
				fmt.Fprintf(cmd.OutOrStdout(), "*Field:* `%s`\n\n", match.Field)
				fmt.Fprintln(cmd.OutOrStdout(), "```javascript")
				for _, lineMatch := range match.LineMatches {
					context := lineMatch.Context
					if len(context) > 80 {
						context = context[:77] + "..."
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%4d: %s\n", int(lineMatch.Line), context)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "```")
				fmt.Fprintln(cmd.OutOrStdout())
			}
		}
	}

	return nil
}
