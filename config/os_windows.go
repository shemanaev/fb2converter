// +build windows

package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// EnableColorOutput checks if colorized output is possible and
// enables proper VT100 sequence processing in Windows 10 console.
func EnableColorOutput(stream *os.File) bool {

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	v, _, err := k.GetIntegerValue("CurrentMajorVersionNumber")
	if err != nil {
		return false
	}

	if v < 10 {
		return false
	}

	if !terminal.IsTerminal(int(stream.Fd())) {
		return false
	}

	var mode uint32
	err = windows.GetConsoleMode(windows.Handle(stream.Fd()), &mode)
	if err != nil {
		return false
	}

	const EnableVirtualTerminalProcessing uint32 = 0x4
	mode |= EnableVirtualTerminalProcessing

	err = windows.SetConsoleMode(windows.Handle(stream.Fd()), mode)
	if err != nil {
		return false
	}
	return true
}

// kindlegen provides OS specific part of default kindlegen location
func kindlegen() string {
	return "kindlegen.exe"
}

// kpv returns OS specific path where Kindle Previewer is installed by default.
func kpv() (string, error) {
	res, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", errors.Wrap(err, "unable to find local AppData folder")
	}
	return filepath.Join(res, "Amazon", "Kindle Previewer 3", "Kindle Previewer 3.exe"), nil
}

// CleanFileName removes not allowed characters form file name.
func CleanFileName(in string) string {
	out := strings.Map(func(sym rune) rune {
		if strings.IndexRune(`<>":/\|?*`+string(os.PathSeparator)+string(os.PathListSeparator), sym) != -1 {
			return -1
		}
		return sym
	}, in)
	if len(out) == 0 {
		out = "_bad_file_name_"
	}
	return out
}

// FindConverter attempts to find main conversion engine - myhomelib support.
func FindConverter(expath string) string {

	expath, err := os.Executable()
	if err != nil {
		return ""
	}

	wd := filepath.Dir(expath)

	paths := []string{
		filepath.Join(wd, "fb2c.exe"),                               // `pwd`
		filepath.Join(filepath.Dir(wd), "fb2converter", "fb2c.exe"), // `pwd`/../fb2converter
	}

	for _, p := range paths {
		if _, err = os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
