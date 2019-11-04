package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/blang/semver"
	"go.uber.org/zap"
)

// ErrNoKPVForOS - not all platforms have Kindle Previewer
var ErrNoKPVForOS = errors.New("kindle previewer is not supported for this OS/platform")

// KPVEnv has everything necessary to run kindle previewer in command line mode and process results.
// NOTE: It is VERY platform specific.
type KPVEnv struct {
	log     *zap.Logger
	kpvVer  semver.Version
	kpvPath string
	sqlVer  semver.Version
	sqlPath string
}

func (e *KPVEnv) String() string {
	return fmt.Sprintf("%s:(%s) %s:(%s)", e.kpvPath, e.kpvVer, e.sqlPath, e.sqlVer)
}

// ExecKPV runs Kindle Previewer with specified arguments.
func (e *KPVEnv) ExecKPV(arg ...string) error {

	var (
		err error
		out []string
	)

	if out, err = kpvexec(e.kpvPath, arg...); err != nil {
		return fmt.Errorf("unable to run kindle previewer [%s]: %w", e.kpvPath, err)
	}
	for _, s := range out {
		if len(s) > 0 {
			e.log.Debug(s)
		}
	}
	return nil
}

// ExecSQL runs sqlite cli shell.
func (e *KPVEnv) ExecSQL(stdin io.Reader, outDir string, arg ...string) error {

	cmd := exec.Command(e.sqlPath, arg...)
	cmd.Stdin = stdin
	cmd.Dir = outDir

	out, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("unable to redirect sqlite stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("unable to start sqlite: %w", err)
	}

	// read and print kindlegen stdout
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		e.log.Debug("sqlite", zap.String("stderr", scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("sqlite stderr pipe broken: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if len(ee.Stderr) > 0 {
				e.log.Error("sqlite", zap.String("stderr", string(ee.Stderr)), zap.Error(err))
			}
			ws := ee.Sys().(syscall.WaitStatus)
			switch ws.ExitStatus() {
			case 0:
				// success
			case 1:
				// warnings
				e.log.Warn("sqlite has some warnings, see log for details")
			case 2:
				// error - unable to dump KDF
				fallthrough
			default:
				return fmt.Errorf("sqlite returned error: %w", err)
			}
		} else {
			return fmt.Errorf("sqlite returned error: %w", err)
		}
	}
	return nil
}

var (
	reKPVver           = regexp.MustCompile(`^Kindle\s+Previewer\s+([0-9]+\.[0-9]+\.[0-9]+)\s+Copyright\s+\(c\)\s+Amazon\.com.*$`)
	minSupportedKPVver = semver.Version{Major: 3, Minor: 32, Patch: 0}
)

// GetKPVEnv initializes KPVEnv.
func (conf *Config) GetKPVEnv(log *zap.Logger) (*KPVEnv, error) {

	var (
		err error
		ver semver.Version
		out []string
	)

	kpath := conf.Doc.KPreViewer.Path
	if len(kpath) > 0 {
		if !filepath.IsAbs(kpath) {
			return nil, fmt.Errorf("path to kindle previewer must be absolute path [%s]", kpath)
		}
	} else {
		kpath, err = kpv()
		if err != nil {
			return nil, fmt.Errorf("problem getting kindle previewer path: %w", err)
		}
	}
	if _, err = os.Stat(kpath); err != nil {
		return nil, fmt.Errorf("unable to find kindle previewer [%s]: %w", kpath, err)
	}

	if out, err = kpvexec(kpath, "-help"); err != nil {
		return nil, fmt.Errorf("unable to run kindle previewer [%s]: %w", kpath, err)
	}

	for _, s := range out {
		matches := reKPVver.FindStringSubmatch(s)
		if len(matches) < 2 {
			continue
		}
		if ver, err = semver.Parse(matches[1]); err != nil {
			return nil, fmt.Errorf("unable to parse kindle previewer version: %w", err)
		}
		break
	}
	if ver.EQ(semver.Version{}) {
		return nil, errors.New("unable to find kindle previewer version")
	}
	if minSupportedKPVver.GT(ver) {
		return nil, fmt.Errorf("unsupported version %s of kindle previewer is installed (required version %s or newer)", ver, minSupportedKPVver)
	}

	// I do not want to use CGO so let's see if we have SQLite cli shell instead
	sqlpath, err := exec.LookPath(sqlite())
	if err != nil {
		return nil, fmt.Errorf("unable to locate sqlite3 cli shell: %w", err)
	}
	sqlpath, err = filepath.Abs(sqlpath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(sqlpath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	sqlver, err := semver.Parse(strings.Split(string(output), " ")[0])
	if err != nil {
		return nil, err
	}
	if sqlver.LT(semver.Version{Major: 3, Minor: 8, Patch: 2}) {
		return nil, fmt.Errorf("SQLite version 3.8.2 or later is necessary in order to use a WITHOUT ROWID table. Found version %s", sqlver)
	}
	kpv := &KPVEnv{
		log:     log,
		kpvVer:  ver,
		kpvPath: kpath,
		sqlVer:  sqlver,
		sqlPath: sqlpath,
	}
	log.Debug("Kindle Previewer 3 found", zap.Stringer("env", kpv))
	return kpv, nil
}
