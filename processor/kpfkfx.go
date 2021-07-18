package processor

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/rupor-github/fb2converter/config"
	"github.com/rupor-github/fb2converter/processor/kfx"
	"github.com/rupor-github/fb2converter/utils"
)

// FinalizeKFX produces final KFX file out of previously saved temporary files.
func (p *Processor) FinalizeKFX(fname string) error {

	outDir := filepath.Join(p.tmpDir, config.DirKfx)
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return fmt.Errorf("unable to create data directories for Kindle Previewer: %w", err)
	}

	kpf, err := p.generateIntermediateKPFContent(outDir)
	if err != nil {
		return fmt.Errorf("unable to generate intermediate content: %w", err)
	}
	if _, err := os.Stat(fname); err == nil {
		if len(p.env.Debug) == 0 && !p.overwrite {
			return fmt.Errorf("output file already exists: %s", fname)
		}
		p.env.Log.Warn("Overwriting existing file", zap.String("file", fname))
		if err = os.Remove(fname); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if err := os.MkdirAll(filepath.Dir(fname), 0700); err != nil {
		return fmt.Errorf("unable to create output directory: %w", err)
	}

	start := time.Now()
	p.env.Log.Debug("Repacking - start")
	defer func(start time.Time) {
		p.env.Log.Debug("Repacking - done",
			zap.Duration("elapsed", time.Since(start)),
			zap.String("file", kpf),
		)
	}(start)

	mover, err := kfx.NewMover(kpf, outDir, p.env.Log)
	if err != nil {
		return fmt.Errorf("unable to parse intermediate content file: %w", err)
	}
	_ = mover
	// if err := splitter.SaveResult(fname); err != nil {
	// 	return fmt.Errorf("unable to save resulting MOBI: %w", err)
	// }
	return fmt.Errorf("FIX ME DONE: %s", fname)
}

// generateIntermediateKPFContent produces temporary KPF file, presently by running Kindle Previewer and returns its full path.
func (p *Processor) generateIntermediateKPFContent(outDir string) (string, error) {

	args := make([]string, 0, 10)
	args = append(args, filepath.Join(p.tmpDir, config.DirEpub, config.DirContent, "content.opf"))
	args = append(args, "-convert")
	args = append(args, "-locale", "en")
	args = append(args, "-output", outDir)

	start := time.Now()
	p.env.Log.Debug("Kindle Previewer - start")
	defer func(start time.Time) {
		p.env.Log.Debug("Kindle Previewer - done",
			zap.Duration("elapsed", time.Since(start)),
			zap.Stringer("env", p.kpvEnv),
			zap.Strings("args", args),
		)
	}(start)

	if err := p.kpvEnv.ExecKPV(args...); err != nil {
		return "", err
	}
	book, err := checkResults(outDir, p.env.Log)
	if err != nil {
		return "", err
	}
	return book, nil
}

func checkResults(outDir string, log *zap.Logger) (string, error) {

	var (
		err     error
		csvFile *os.File
		csvName = filepath.Join(outDir, "Summary_Log.csv")
	)

	if csvFile, err = os.Open(csvName); err != nil {
		return "", fmt.Errorf("unable to open conversion summary: %w", err)
	}
	defer csvFile.Close()

	const (
		hdrBookName int = iota // "Book Name" - input
		hdrETStatus            // "Enhanced Typesetting Status"
		hdrStatus              // "Conversion Status"
		hdrErrors              // "Error Count"
		hdrWarnings            // "Warning Count"
		hdrInfo                // "Quality Issue Count"
		hdrBook                // "Output File Path"
		hdrLog                 // "Log File Path"
		hdrReport              // "Quality Report Path"
	)

	enc, err := utils.DetectFileUTF(csvFile)
	if err != nil {
		return "", fmt.Errorf("unable to read conversion summary: %w", err)
	}

	r := csv.NewReader(utils.SelectReader(csvFile, enc))
	r.FieldsPerRecord = 0

	records, err := r.ReadAll()
	if err != nil {
		return "", fmt.Errorf("unable to parse conversion summary: %w", err)
	}
	if len(records) != 2 {
		return "", fmt.Errorf("wrong number of summary lines: %d", len(records))
	}

	headers := records[0]
	record := records[1]

	var fields = []zap.Field{}
	for i := 0; i < len(headers); i++ {
		fields = append(fields, zap.String(headers[i], record[i]))
	}
	log.Info("KPV summary", fields...)

	// TODO: additional version dependent log:  if len(record[hdrLog]) > 0

	// Various superficial checks, mostly for clarity
	if !strings.EqualFold(record[hdrETStatus], "Supported") {
		return "", fmt.Errorf("wrong Enhanced Typesetting Status: %s", record[hdrETStatus])
	}
	if !strings.EqualFold(record[hdrStatus], "Success") {
		return "", fmt.Errorf("wrong Conversion Status: %s", record[hdrStatus])
	}
	if !strings.EqualFold(record[hdrErrors], "0") {
		return "", errors.New("errors during conversion, see log for details")
	}
	if len(record[hdrBook]) == 0 {
		return "", errors.New("unable to detect resulting KPF, path is empty")
	}
	if _, err = os.Stat(record[hdrBook]); err != nil {
		return "", fmt.Errorf("unable to find resulting KPF file [%s]: %w", record[hdrBook], err)
	}
	return record[hdrBook], nil
}
