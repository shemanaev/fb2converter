// +build darwin

package config

import (
	"os"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

// EnableColorOutput checks if colorized output is possible.
func EnableColorOutput(stream *os.File) bool {
	return terminal.IsTerminal(int(stream.Fd()))
}

// kindlegen provides OS specific part of default kindlegen location
func kindlegen() string {
	return "kindlegen"
}

// kpv returns OS specific path where Kindle Previewer is installed by default.
func kpv() (string, error) {
	return "", ErrNoKPVForOS
}

// CleanFileName removes not allowed characters form file name.
func CleanFileName(in string) string {
	out := strings.TrimLeft(strings.Map(func(sym rune) rune {
		if strings.IndexRune(string(os.PathSeparator)+string(os.PathListSeparator), sym) != -1 {
			return -1
		}
		return sym
	}, in), ".")
	if len(out) == 0 {
		out = "_bad_file_name_"
	}
	return out
}

// FindConverter  - used on Windows to support myhomelib
func FindConverter(_ string) string {
	return ""
}
