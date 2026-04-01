package display

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ANSI color codes
const (
	ansiRed    = "\033[0;31m"
	ansiGreen  = "\033[0;32m"
	ansiYellow = "\033[1;33m"
	ansiBlue   = "\033[0;34m"
	ansiCyan   = "\033[0;36m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiReset  = "\033[0m"
)

func ColorRed(s string) string    { return ansiRed + s + ansiReset }
func ColorGreen(s string) string  { return ansiGreen + s + ansiReset }
func ColorYellow(s string) string { return ansiYellow + s + ansiReset }
func ColorBlue(s string) string   { return ansiBlue + s + ansiReset }
func ColorCyan(s string) string   { return ansiCyan + s + ansiReset }
func ColorBold(s string) string   { return ansiBold + s + ansiReset }
func ColorDim(s string) string    { return ansiDim + s + ansiReset }

func Timestamp() string {
	return time.Now().Format("15:04:05")
}

func LogInfo(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", ColorBlue("["+Timestamp()+"]"), msg)
}

func LogSuccess(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", ColorGreen("["+Timestamp()+"] OK"), msg)
}

func LogWaiting(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", ColorYellow("["+Timestamp()+"] .."), msg)
}

func LogErr(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", ColorRed("["+Timestamp()+"] ERR"), msg)
}

func LogBold(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", ColorBold(msg))
}

func LogDim(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", ColorDim(msg))
}

func Header(title string) {
	line := strings.Repeat("=", 55)
	fmt.Fprintf(os.Stderr, "\n%s\n", ColorBold(ColorCyan(line)))
	fmt.Fprintf(os.Stderr, "%s\n", ColorBold(ColorCyan("  "+title)))
	fmt.Fprintf(os.Stderr, "%s\n\n", ColorBold(ColorCyan(line)))
}

func Divider() {
	fmt.Fprintf(os.Stderr, "%s\n", ColorDim(strings.Repeat("-", 55)))
}

func FormatDuration(d time.Duration) string {
	total := int(d.Seconds())
	mins := total / 60
	secs := total % 60
	if mins > 0 {
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

func CountLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func PromptPreview(s string, maxLines int) string {
	lines := strings.SplitN(s, "\n", maxLines+1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	var result []string
	for _, l := range lines {
		result = append(result, "    "+l)
	}
	return strings.Join(result, "\n")
}

func JoinStrings(ss []string, sep string) string {
	return strings.Join(ss, sep)
}

func HumanSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fK", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fM", float64(bytes)/(1024*1024))
}
