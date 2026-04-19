package commands

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/jacebenson/jsn/internal/appctx"
	"github.com/jacebenson/jsn/internal/auth"
	"github.com/jacebenson/jsn/internal/config"
	"github.com/jacebenson/jsn/internal/output"
	"github.com/jacebenson/jsn/internal/sdk"
	"github.com/jacebenson/jsn/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewSetupCommand creates the setup command for interactive first-time setup.
func NewSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time setup",
		Long: `Walk through instance configuration and authentication setup.

This command will guide you through:
  1. Instance URL configuration
  2. Authentication setup (OAuth, Basic Auth, or g_ck token)
  3. Saving your configuration`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := appctx.FromContext(cmd.Context())
			return runSetup(cmd, app)
		},
	}

	return cmd
}

func runSetup(cmd *cobra.Command, app *appctx.App) error {
	if app == nil {
		return fmt.Errorf("setup cannot run: application context not initialized\n\nThis usually means there's an issue with the config system.\nTry one of the following:\n  1. Run 'jsn setup' directly instead of through another command\n  2. Delete ~/.config/servicenow/config.json and run 'jsn setup'\n  3. Check file permissions on your config directory")
	}

	fmt.Println()
	fmt.Println("→ JSN Setup")
	fmt.Println()

	cfg := app.Config.(*config.Config)

	// Step 1: Instance URL
	instanceURL, err := setupInstanceURL(cfg)
	if err != nil {
		return err
	}

	// Step 2: Profile name
	profileName, err := setupProfileName(cfg)
	if err != nil {
		return err
	}

	// Step 3: Save Location and Auth
	fmt.Println("Save Location")

	locationItems := []tui.PickerItem{
		{ID: "local", Title: "Local", Description: ".servicenow/config.json — project-specific"},
		{ID: "global", Title: "Global", Description: "~/.config/servicenow/config.json — system-wide"},
	}
	locationChoice, err := tui.Pick("Save location", locationItems)
	if err != nil {
		return err
	}
	if locationChoice == nil {
		return nil
	}
	configScope := locationChoice.ID
	fmt.Println()

	authItems := []tui.PickerItem{
		{ID: "basic", Title: "Basic Auth", Description: "username/password"},
		{ID: "oauth", Title: "OAuth 2.0", Description: "browser-based, most secure"},
		{ID: "gck", Title: "g_ck Token", Description: "glide cookie from browser"},
	}
	authChoice, err := tui.Pick("Authentication method", authItems)
	if err != nil {
		return err
	}
	if authChoice == nil {
		return nil
	}
	authMethod := authChoice.ID
	fmt.Println()

	authManager := app.Auth.(*auth.Manager)

	for {
		// Run the chosen auth flow
		authErr := runAuthMethod(cmd, app, authMethod, configScope, instanceURL, profileName)
		if authErr != nil {
			fmt.Println()
			fmt.Printf("  ⚠ %v\n", authErr)
			fmt.Println()

			retryItems := []tui.PickerItem{
				{ID: "retry", Title: "Try again", Description: "re-enter credentials for " + authMethod},
				{ID: "change", Title: "Different auth method", Description: "switch to another method"},
				{ID: "start-over", Title: "Start over", Description: "restart setup from the beginning"},
				{ID: "quit", Title: "Quit", Description: "exit setup"},
			}
			choice, pickErr := tui.Pick("Authentication failed", retryItems)
			if pickErr != nil || choice == nil || choice.ID == "quit" {
				return nil
			}
			if choice.ID == "start-over" {
				return runSetup(cmd, app)
			}
			if choice.ID == "change" {
				fmt.Println()
				newChoice, pickErr := tui.Pick("Authentication method", authItems)
				if pickErr != nil || newChoice == nil {
					return nil
				}
				authMethod = newChoice.ID
				fmt.Println()
			}
			continue
		}

		// Verify authentication by fetching current user
		fmt.Printf("  Verifying authentication against %s...\n", instanceURL)
		testClient := sdk.NewClient(instanceURL, func() (string, string, string) {
			creds, err := authManager.GetCredentials()
			if err != nil {
				return "", "", "basic"
			}
			profile := cfg.GetActiveProfile()
			am := ""
			if profile != nil {
				am = profile.AuthMethod
			}
			if creds.AuthMethod != "" {
				am = creds.AuthMethod
			}
			switch am {
			case "oauth":
				return creds.AccessToken, "", "oauth"
			case "gck":
				return creds.Token, creds.Cookies, "gck"
			default:
				return creds.Token, creds.Username, "basic"
			}
		})

		user, userErr := testClient.GetCurrentUser(context.Background())
		if userErr != nil {
			fmt.Println()
			fmt.Printf("  ⚠ Verification failed: %v\n", userErr)
			fmt.Println()

			retryItems := []tui.PickerItem{
				{ID: "retry", Title: "Try again", Description: "re-enter credentials for " + authMethod},
				{ID: "change", Title: "Different auth method", Description: "switch to another method"},
				{ID: "continue", Title: "Continue anyway", Description: "credentials saved, verify later"},
				{ID: "quit", Title: "Quit", Description: "exit setup"},
			}
			choice, pickErr := tui.Pick("Could not verify authentication", retryItems)
			if pickErr != nil || choice == nil || choice.ID == "quit" {
				return nil
			}
			if choice.ID == "continue" {
				break
			}
			if choice.ID == "change" {
				fmt.Println()
				newChoice, pickErr := tui.Pick("Authentication method", authItems)
				if pickErr != nil || newChoice == nil {
					return nil
				}
				authMethod = newChoice.ID
				fmt.Println()
			}
			continue
		}

		userName := user.Name
		if userName == "" {
			userName = user.UserName
		}
		fmt.Printf("  ✓ Authenticated as: %s\n", userName)
		break
	}

	// Show completion
	fmt.Println()
	fmt.Println("✓ Setup complete!")
	fmt.Println()
	fmt.Printf("  Instance: %s\n", instanceURL)
	fmt.Printf("  Profile:  %s\n", profileName)
	fmt.Println()

	// Find the jsn binary to show correct command examples
	jsnCmd := findJSNBinary()

	fmt.Println("Next steps:")
	fmt.Printf("  %s records --table change_request   Browse change requests\n", jsnCmd)
	fmt.Printf("  %s rules --table incident           Business rules\n", jsnCmd)
	fmt.Printf("  %s flows                            Explore flows\n", jsnCmd)
	fmt.Println()

	return nil
}

func setupInstanceURL(cfg *config.Config) (string, error) {
	fmt.Println("Instance URL")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  https://mycompany.service-now.com")
	fmt.Println("  https://dev12345.service-now.com")
	fmt.Println()

	// Check if we already have profiles
	var defaultURL string
	if len(cfg.Profiles) > 0 {
		for _, profile := range cfg.Profiles {
			defaultURL = profile.InstanceURL
			break
		}
	}

	var instanceURL string
	reader := bufio.NewReader(os.Stdin)
	for {
		if defaultURL != "" {
			fmt.Printf("Instance URL [%s]: ", defaultURL)
		} else {
			fmt.Print("Instance URL: ")
		}

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" && defaultURL != "" {
			instanceURL = defaultURL
			break
		}

		if input == "" {
			fmt.Println("Instance URL is required.")
			continue
		}

		instanceURL = normalizeInstanceURL(input)
		break
	}

	fmt.Printf("  ✓ Using: %s\n", instanceURL)
	fmt.Println()
	return instanceURL, nil
}

func setupProfileName(cfg *config.Config) (string, error) {
	fmt.Println("Profile Name")
	fmt.Println()
	fmt.Println("Examples: prod, dev, sandbox")
	fmt.Println()

	// Suggest a default based on instance URL
	defaultName := "default"
	if cfg.DefaultProfile != "" {
		defaultName = cfg.DefaultProfile
	}

	fmt.Printf("Profile name [%s]: ", defaultName)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var profileName string
	if input == "" {
		profileName = defaultName
	} else {
		profileName = input
	}

	fmt.Printf("  ✓ Profile: %s\n", profileName)
	fmt.Println()
	return profileName, nil
}

func runAuthMethod(cmd *cobra.Command, app *appctx.App, authMethod, configScope, instanceURL, profileName string) error {
	reader := bufio.NewReader(os.Stdin)
	authManager := app.Auth.(*auth.Manager)
	cfg := app.Config.(*config.Config)

	switch authMethod {
	case "basic":
		return setupBasicAuth(reader, cfg, authManager, instanceURL, profileName, configScope)
	case "oauth":
		return setupOAuthAuth(cfg, authManager, instanceURL, profileName, configScope)
	default:
		return setupGCKAuth(reader, cfg, authManager, instanceURL, profileName, configScope)
	}
}

func setupBasicAuth(reader *bufio.Reader, cfg *config.Config, authManager *auth.Manager, instanceURL, profileName, configScope string) error {
	fmt.Println("Basic Authentication")
	fmt.Println()
	fmt.Println("Enter your ServiceNow username and password.")
	fmt.Println()

	// Username
	var username string
	for {
		fmt.Print("Username: ")
		input, _ := reader.ReadString('\n')
		username = strings.TrimSpace(input)
		if username != "" {
			break
		}
		fmt.Println("Username is required.")
	}

	// Password input
	var password string
	for {
		if term.IsTerminal(int(syscall.Stdin)) {
			fmt.Print("Password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				// Fallback to regular input
				input, _ := reader.ReadString('\n')
				password = strings.TrimSpace(input)
			} else {
				password = string(bytePassword)
				fmt.Println(" ********")
			}
		} else {
			// Non-terminal mode - prompt without "Password: " prefix since it was already printed
			input, _ := reader.ReadString('\n')
			password = strings.TrimSpace(input)
		}

		if password != "" {
			break
		}
		if term.IsTerminal(int(syscall.Stdin)) {
			fmt.Println("Password is required.")
		}
	}

	// Save profile with auth method
	profile := &config.Profile{
		InstanceURL: instanceURL,
		Username:    username,
		AuthMethod:  "basic",
	}

	cfg.Profiles[profileName] = profile
	// Always set the default profile to the newly created one during setup
	cfg.DefaultProfile = profileName

	// Save to appropriate location
	var saveErr error
	if configScope == "local" {
		saveErr = cfg.SaveLocal()
	} else {
		saveErr = cfg.Save()
	}
	if saveErr != nil {
		return output.ErrAPI(500, fmt.Sprintf("failed to save config: %v", saveErr))
	}

	// Store credentials
	creds := &auth.Credentials{
		Token:     password,
		Username:  username,
		CreatedAt: 0,
	}

	if err := authManager.StoreCredentials(creds); err != nil {
		return output.ErrAuth(fmt.Sprintf("failed to store credentials: %v", err))
	}

	fmt.Println()
	fmt.Println("  ✓ Basic auth credentials saved.")
	fmt.Println()

	return nil
}

func setupOAuthAuth(cfg *config.Config, authManager *auth.Manager, instanceURL, profileName, configScope string) error {
	fmt.Println("OAuth 2.0 Authentication")
	fmt.Println()

	// Run OAuth flow
	creds, err := auth.OAuthFlow(instanceURL)
	if err != nil {
		return output.ErrAuth(fmt.Sprintf("OAuth authentication failed: %v", err))
	}

	// Save profile with auth method
	profile := &config.Profile{
		InstanceURL: instanceURL,
		AuthMethod:  "oauth",
	}

	cfg.Profiles[profileName] = profile
	// Always set the default profile to the newly created one during setup
	cfg.DefaultProfile = profileName

	// Save to appropriate location
	var saveErr error
	if configScope == "local" {
		saveErr = cfg.SaveLocal()
	} else {
		saveErr = cfg.Save()
	}
	if saveErr != nil {
		return output.ErrAPI(500, fmt.Sprintf("failed to save config: %v", saveErr))
	}

	// Store credentials
	creds.AuthMethod = "oauth"
	if err := authManager.StoreCredentials(creds); err != nil {
		return output.ErrAuth(fmt.Sprintf("failed to store credentials: %v", err))
	}

	fmt.Println()
	fmt.Println("  ✓ OAuth credentials saved.")
	fmt.Println()

	return nil
}

func setupGCKAuth(reader *bufio.Reader, cfg *config.Config, authManager *auth.Manager, instanceURL, profileName, configScope string) error {
	fmt.Println("g_ck Token Authentication")
	fmt.Println()
	fmt.Println("To authenticate, paste a curl command from your browser.")
	fmt.Println()
	fmt.Println("Steps:")
	fmt.Println("  1. Log into your ServiceNow instance in a browser")
	fmt.Println("  2. Open DevTools (F12) → Network tab")
	fmt.Println("  3. Filter for API requests (type 'api' in the filter)")
	fmt.Println("  4. Right-click any api/now/* request")
	fmt.Println("  5. Select: Copy → Copy as cURL")
	fmt.Println("  6. Paste the command below and press Enter")
	fmt.Println()

	curlCmd, err := readCurlHidden(reader)
	if err != nil {
		return output.ErrUsage(err.Error())
	}

	if curlCmd == "" {
		return output.ErrUsage("no input received")
	}

	// Parse the curl command
	token, cookies, err := parseCurlForAuth(curlCmd)
	if err != nil {
		return output.ErrUsage(fmt.Sprintf("failed to parse curl: %v", err))
	}

	if token == "" {
		return output.ErrUsage("no X-UserToken found in curl command")
	}
	if cookies == "" {
		return output.ErrUsage("no Cookie header found in curl command")
	}

	// Save profile with auth method
	profile := &config.Profile{
		InstanceURL: instanceURL,
		AuthMethod:  "gck",
	}

	cfg.Profiles[profileName] = profile
	// Always set the default profile to the newly created one during setup
	cfg.DefaultProfile = profileName

	// Save to appropriate location
	var saveErr error
	if configScope == "local" {
		saveErr = cfg.SaveLocal()
	} else {
		saveErr = cfg.Save()
	}
	if saveErr != nil {
		return output.ErrAPI(500, fmt.Sprintf("failed to save config: %v", saveErr))
	}

	// Store credentials
	creds := &auth.Credentials{
		Token:     token,
		Cookies:   filterServiceNowCookies(cookies),
		CreatedAt: 0,
	}

	if err := authManager.StoreCredentials(creds); err != nil {
		return output.ErrAuth(fmt.Sprintf("failed to store credentials: %v", err))
	}

	fmt.Println()
	fmt.Println("  ✓ Credentials saved.")
	fmt.Println()

	return nil
}

// findJSNBinary attempts to locate the jsn executable for displaying
// the correct command examples. It checks in order:
// 1. Current executable path (os.Executable)
// 2. Current directory
// 3. PATH (using which/where)
// Returns "jsn" if not found, or the appropriate path/command to use
func findJSNBinary() string {
	// First, try to get the current executable path
	if exePath, err := os.Executable(); err == nil {
		// If it's already just "jsn" or "jsn.exe", it's in PATH
		if filepath.Base(exePath) == exePath {
			return "jsn"
		}
		// If the directory is in PATH, we can just use "jsn"
		exeDir := filepath.Dir(exePath)
		if pathInPATH(exeDir) {
			return "jsn"
		}
		// Return the full path as fallback
		return exePath
	}

	// Check current directory
	if runtime.GOOS == "windows" {
		if _, err := os.Stat("jsn.exe"); err == nil {
			return ".\\jsn.exe"
		}
	} else {
		if _, err := os.Stat("jsn"); err == nil {
			return "./jsn"
		}
	}

	// Try to find in PATH using which/where
	lookCmd := "which"
	if runtime.GOOS == "windows" {
		lookCmd = "where"
	}
	if path, err := exec.LookPath("jsn"); err == nil {
		// If found in PATH, just return "jsn"
		_ = lookCmd // suppress unused warning
		_ = path
		return "jsn"
	}

	// Default fallback
	return "jsn"
}

// pathInPATH checks if a directory is in the system's PATH
func pathInPATH(dir string) bool {
	pathEnv := os.Getenv("PATH")
	separator := string(filepath.ListSeparator)
	for _, p := range strings.Split(pathEnv, separator) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// joinCurlLines joins multi-line curl input, stripping shell line continuations.
func joinCurlLines(lines []string) string {
	var sb strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r\n")
		if strings.HasSuffix(trimmed, "\\") {
			sb.WriteString(strings.TrimSuffix(trimmed, "\\"))
			sb.WriteByte(' ')
		} else {
			sb.WriteString(trimmed)
			sb.WriteByte(' ')
		}
	}
	return strings.TrimSpace(sb.String())
}

// parseCurlForAuth extracts auth info from a curl command
func parseCurlForAuth(curlCmd string) (token, cookies string, err error) {
	// Extract X-UserToken header (case insensitive)
	tokenPatterns := []string{
		`(?i)x-usertoken:\s*([^\s'"]+)`,
		`-H\s+['"]X-UserToken:\s*([^'"]+)['"]`,
	}
	for _, pattern := range tokenPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(curlCmd)
		if len(matches) >= 2 {
			token = strings.TrimSpace(matches[1])
			break
		}
	}

	// Extract Cookie header (from -H or -b flags)
	// Chrome's "Copy as cURL" uses: -b 'cookie1=val1; cookie2=val2'
	// The -b pattern must handle quoted strings containing spaces and semicolons
	cookiePatterns := []string{
		`(?i)-H\s+['"]cookie:\s*([^'"]+)['"]`,
		`-b\s+'([^']+)'`,
		`-b\s+"([^"]+)"`,
		`-b\s+(\S+)`,
	}
	for _, pattern := range cookiePatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(curlCmd)
		if len(matches) >= 2 {
			cookies = strings.TrimSpace(matches[1])
			break
		}
	}

	if token == "" && cookies == "" {
		// Try to extract Basic Auth
		authMatch := regexp.MustCompile(`-H\s+['"]Authorization:\s*Basic\s+([^'"]+)['"]`).FindStringSubmatch(curlCmd)
		if len(authMatch) >= 2 {
			decoded, decodeErr := base64.StdEncoding.DecodeString(authMatch[1])
			if decodeErr == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					return parts[1], "", nil // password is the token
				}
			}
		}
	}

	return token, cookies, nil
}
