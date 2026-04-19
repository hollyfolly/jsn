// Package output provides terminal output utilities including OSC 8 hyperlinks.
package output

import (
	"fmt"
	"os"
)

// IsTerminal returns true if the given file descriptor is a terminal.
func IsTerminal(fd uintptr) bool {
	stat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// OSC8Hyperlink creates an OSC 8 escape sequence for terminal hyperlinks.
// Returns plain text if the terminal doesn't support hyperlinks or output is not a terminal.
// Format: \x1b]8;;URL\x1b\\TEXT\x1b]8;;\x1b\\
func OSC8Hyperlink(url, text string) string {
	// OSC 8 escape sequences
	oscStart := "\x1b]8;;"
	oscEnd := "\x1b\\"

	return fmt.Sprintf("%s%s%s%s%s%s", oscStart, url, oscEnd, text, oscStart, oscEnd)
}

// OSC8HyperlinkIfTerminal creates a hyperlink only if output is a terminal.
func OSC8HyperlinkIfTerminal(url, text string, isTerminal bool) string {
	if !isTerminal {
		return text
	}
	return OSC8Hyperlink(url, text)
}

// TruncateString truncates a string to the specified maximum length.
// If the string is longer than maxLen, it will be truncated and "..." added.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
