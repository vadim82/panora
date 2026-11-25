package cli

import (
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/mattn/go-isatty"
)

func colorEnabled(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	if term := os.Getenv("TERM"); term == "" || term == "dumb" {
		return false
	}
	file, ok := out.(interface{ Fd() uintptr })
	return ok && isatty.IsTerminal(file.Fd())
}

func styled(out io.Writer, color, text string) string {
	if !colorEnabled(out) {
		return text
	}
	return "\x1b[" + color + "m" + text + "\x1b[0m"
}

// terminalText prevents paths, titles and diagnostics from controlling the terminal.
func terminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return '�'
		}
		return r
	}, value)
}
