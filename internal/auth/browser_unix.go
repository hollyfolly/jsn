//go:build !windows && !darwin
// +build !windows,!darwin

package auth

import (
	"os/exec"
	"runtime"
)

// openBrowserCommand opens the specified URL in the default browser
func openBrowserCommand(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		// Try xdg-open first, then fallback to common browsers
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
