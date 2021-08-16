//go:build windows

package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rupor-github/fb2converter/config/winpty"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/term"
)

// CheckPath is called to make sure that path for storing debug related artifacts is OK.
func CheckPath(path string) error {
	if strings.HasPrefix(path, `\\wsl$\`) {
		ms := 20
		if len(path) < ms {
			ms = len(path)
		}
		return fmt.Errorf("windows build of converter has problems accessing files in debug directory using P9 prototcol [%s...], use --debug-dir to specify better location", path[:ms])
	}
	return nil
}

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

	if !term.IsTerminal(int(stream.Fd())) {
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
	return err == nil
}

// CleanFileName removes not allowed characters form file name.
func CleanFileName(in string) string {
	out := strings.Map(func(sym rune) rune {
		if strings.ContainsRune(`<>":/\|?*`+string(os.PathSeparator)+string(os.PathListSeparator), sym) {
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

	var err error
	if len(expath) == 0 {
		expath, err = os.Executable()
		if err != nil {
			return ""
		}
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

// kindlegen provides OS specific part of default kindlegen location
func kindlegen() string {
	return "kindlegen.exe"
}

// kpvDefault returns OS specific path where Kindle Previewer is installed by default.
func kpvDefault() (string, error) {
	res, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("unable to find local AppData folder: %w", err)
	}
	return filepath.Join(res, "Amazon", "Kindle Previewer 3", "Kindle Previewer 3.exe"), nil
}

// kpvExec executes kpv from provided path using winpty to intercept output.
// NOTE: on Windows kpv attaches to console and directly writes to screen buffer, so reading stdout does not work.
func kpvExec(exepath string, arg ...string) ([]string, error) {

	expath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get program path: %w", err)
	}

	pty, err := winpty.New(filepath.Dir(expath))
	if err != nil {
		return nil, fmt.Errorf("failed to allocate winpty: %w", err)
	}

	err = pty.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open winpty: %w", err)
	}
	defer pty.Close()

	_ = pty.SetWinsize(200, 60)

	out := make([]string, 0, 32)
	go func() {
		// read kpv stdout
		scanner := bufio.NewScanner(pty.StdOut)
		for scanner.Scan() {
			out = append(out, scanner.Text())
		}
	}()

	cmd := exec.Command(exepath, arg...)
	err = pty.Run(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to run winpty: %w", err)
	}

	err = pty.Wait()
	if err != nil {
		var exitCode uint32
		winptyError, ok := err.(*winpty.ExitError)
		if ok {
			exitCode = winptyError.WaitStatus.ExitCode
		} else {
			exitError, ok := err.(*exec.ExitError)
			if !ok {
				return nil, fmt.Errorf("kindle previewer failed with unexpected error: %w", err)
			}
			waitStatus, ok := exitError.Sys().(syscall.WaitStatus)
			if !ok {
				return nil, fmt.Errorf("kindle previewer failed with unexpected status: %w", err)
			}
			if waitStatus.Signaled() {
				return nil, errors.New("kindle previewer was interrupted")
			}
			exitCode = uint32(waitStatus.ExitStatus())
		}
		return nil, fmt.Errorf("kindle previewer ended with code %d", exitCode)
	}
	return out, nil
}
