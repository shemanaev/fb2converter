package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/blang/semver"
	"go.uber.org/zap"
)

var (
	// ErrNoKPVForOS - not all platforms have Kindle Previewer.
	ErrNoKPVForOS = errors.New("kindle previewer is not supported for this OS/platform")

	reKPVver           = regexp.MustCompile(`^Kindle\s+Previewer\s+([0-9]+\.[0-9]+\.[0-9]+)\s+Copyright\s+\(c\)\s+Amazon\.com.*$`)
	minSupportedKPVver = semver.Version{Major: 3, Minor: 38, Patch: 0}
)

// KPVEnv has everything necessary to run kindle previewer in command line mode and process results.
type KPVEnv struct {
	log     *zap.Logger
	kpvVer  semver.Version
	kpvPath string
}

func (e *KPVEnv) String() string {
	return fmt.Sprintf("%s:(%s)", e.kpvPath, e.kpvVer)
}

// ExecKPV runs Kindle Previewer with specified arguments.
func (e *KPVEnv) ExecKPV(arg ...string) error {

	var (
		err error
		out []string
	)

	if out, err = kpvExec(e.kpvPath, arg...); err != nil {
		return fmt.Errorf("unable to run kindle previewer [%s]: %w", e.kpvPath, err)
	}
	for _, s := range out {
		if len(s) > 0 {
			e.log.Debug(s)
		}
	}
	return nil
}

// NewKPVEnv initializes KPVEnv.
func (conf *Config) NewKPVEnv(log *zap.Logger) (*KPVEnv, error) {

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
		kpath, err = kpvDefault()
		if err != nil {
			return nil, fmt.Errorf("problem getting kindle previewer path: %w", err)
		}
	}
	if _, err = os.Stat(kpath); err != nil {
		return nil, fmt.Errorf("unable to find kindle previewer [%s]: %w", kpath, err)
	}

	if out, err = kpvExec(kpath, "-help"); err != nil {
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

	kpv := &KPVEnv{
		log:     log,
		kpvVer:  ver,
		kpvPath: kpath,
	}
	log.Debug("Kindle Previewer 3 found", zap.Stringer("env", kpv))
	return kpv, nil
}
