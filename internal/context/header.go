// Package context provides contextual information display for the CLI.
package context

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jacebenson/jsn/internal/config"
	"github.com/jacebenson/jsn/internal/output"
	"github.com/jacebenson/jsn/internal/sdk"
)

// Info holds contextual information about the current session.
type Info struct {
	ProfileName   string
	UserName      string
	UserSysID     string
	ScopeName     string
	UpdateSetName string
	InstanceURL   string
}

// GetInfo retrieves contextual information from the current session.
func GetInfo(cfg *config.Config, sdkClient *sdk.Client) (*Info, error) {
	profile := cfg.GetActiveProfile()
	if profile == nil {
		return nil, fmt.Errorf("no active profile")
	}

	info := &Info{
		ProfileName: cfg.DefaultProfile,
		InstanceURL: profile.InstanceURL,
	}

	if info.ProfileName == "" {
		info.ProfileName = extractInstanceName(profile.InstanceURL)
	}

	// Get current user
	user, err := sdkClient.GetCurrentUser(context.Background())
	if err == nil && user != nil {
		info.UserName = user.Name
		if info.UserName == "" {
			info.UserName = user.UserName
		}
		info.UserSysID = user.SysID
	} else {
		info.UserName = "Unknown"
	}

	// Get current scope
	if info.UserSysID != "" {
		app, err := sdkClient.GetCurrentApplication(context.Background(), info.UserSysID)
		if err == nil && app != nil && app.Scope != "" {
			info.ScopeName = app.Scope
		} else {
			info.ScopeName = "global"
		}
	} else {
		info.ScopeName = "global"
	}

	// Get current update set
	if info.UserSysID != "" {
		updateSet, err := sdkClient.GetCurrentUpdateSet(context.Background(), info.UserSysID)
		if err == nil && updateSet != nil {
			info.UpdateSetName = updateSet.Name
		} else {
			info.UpdateSetName = "-"
		}
	} else {
		info.UpdateSetName = "-"
	}

	return info, nil
}

// PrintHeader prints the contextual header showing profile, user, scope, and update set.
// This replaces the old default update set warning with a more informative header.
func PrintHeader(w io.Writer, cfg *config.Config, sdkClient *sdk.Client) error {
	info, err := GetInfo(cfg, sdkClient)
	if err != nil {
		return err
	}

	// Shorten user name to 6 characters for display
	displayUserName := info.UserName
	if len(displayUserName) > 6 {
		displayUserName = displayUserName[:6]
	}

	// Build links
	userLink := fmt.Sprintf("%s/sys_user_list.do?sysparm_query=sys_id=%s", info.InstanceURL, info.UserSysID)
	scopeLink := fmt.Sprintf("%s/sys_scope.do?sysparm_query=scope=%s", info.InstanceURL, info.ScopeName)
	updateSetLink := ""

	// Get update set sys_id for the link
	updateSet, _ := sdkClient.GetCurrentUpdateSet(context.Background(), info.UserSysID)
	if updateSet != nil {
		updateSetLink = fmt.Sprintf("%s/sys_update_set.do?sys_id=%s", info.InstanceURL, updateSet.SysID)
	}

	// Calculate column widths dynamically
	profileWidth := max(len("PROFILE"), len(info.ProfileName))
	userWidth := max(len("USER"), len(displayUserName))
	scopeDisplay := fmt.Sprintf("[%s]", info.ScopeName)
	scopeWidth := max(len("[SCOPE]"), len(scopeDisplay))

	// Print contextual header
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true)

	// Build header with minimal spacing
	header := fmt.Sprintf("%-*s %-*s %-*s %s", profileWidth, "PROFILE", userWidth, "USER", scopeWidth, "[SCOPE]", "UPDATE SET")
	fmt.Fprintln(w, hintStyle.Render("# Use `jsn updateset use` or `jsn scope use` to change scope/updateset"))
	fmt.Fprintln(w, headerStyle.Render(header))

	// Build clickable data row with OSC 8 hyperlinks
	profileLink := output.OSC8Hyperlink(info.InstanceURL, fmt.Sprintf("%-*s", profileWidth, info.ProfileName))
	userLinkStr := output.OSC8Hyperlink(userLink, fmt.Sprintf("%-*s", userWidth, displayUserName))
	scopeLinkStr := output.OSC8Hyperlink(scopeLink, fmt.Sprintf("%-*s", scopeWidth, scopeDisplay))
	updateSetLinkStr := info.UpdateSetName
	if updateSetLink != "" {
		updateSetLinkStr = output.OSC8Hyperlink(updateSetLink, info.UpdateSetName)
	}

	dataRow := fmt.Sprintf("%s %s %s %s", profileLink, userLinkStr, scopeLinkStr, updateSetLinkStr)
	fmt.Fprintln(w, dataRow)

	return nil
}

// extractInstanceName extracts the instance name from a ServiceNow URL.
func extractInstanceName(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, "/")

	parts := strings.Split(url, "/")
	host := parts[0]

	host = strings.TrimSuffix(host, ".service-now.com")
	host = strings.TrimSuffix(host, ".servicenowservices.com")

	return host
}

// max returns the maximum of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
