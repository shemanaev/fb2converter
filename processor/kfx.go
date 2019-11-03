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

	"github.com/rupor-github/fb2converter/utils"
)

// FinalizeKFX produces final KFX file out of previously saved temporary files.
func (p *Processor) FinalizeKFX(fname string) error {

	kpffile, err := p.generateIntermediateKPFContent(fname)
	if err != nil {
		return fmt.Errorf("unable to generate intermediate content: %w", err)
	}
	return fmt.Errorf("FIX ME DONE: %s", kpffile)
}

// generateIntermediateKPFContent produces temporary KPF file, presently by running Kindle Previewer and returns its full path.
func (p *Processor) generateIntermediateKPFContent(fname string) (string, error) {

	outDir := filepath.Join(p.tmpDir, DirKfx)
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return "", fmt.Errorf("unable to create data directories for Kindle Previewer: %w", err)
	}

	args := make([]string, 0, 10)
	args = append(args, filepath.Join(p.tmpDir, DirEpub, DirContent, "content.opf"))
	args = append(args, "-convert")
	args = append(args, "-locale", "en")
	args = append(args, "-output", outDir)

	start := time.Now()
	p.env.Log.Debug("Kindle Previewer is staring")
	defer func(start time.Time) {
		p.env.Log.Debug("Kindle Previewer is done",
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
		hdrWarnings            // "Warning Count"
		hdrErrors              // "Error Count"
		hdrBook                // "Output File Path"
		hdrLog                 // "Log File Path"
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

	log.Info("KPV summary",
		zap.String(headers[hdrETStatus], record[hdrETStatus]),
		zap.String(headers[hdrStatus], record[hdrStatus]),
		zap.String(headers[hdrWarnings], record[hdrWarnings]),
		zap.String(headers[hdrErrors], record[hdrErrors]),
		zap.String(headers[hdrBook], record[hdrBook]),
		zap.String(headers[hdrLog], record[hdrLog]),
	)
	if len(record[hdrLog]) > 0 {
		logDetails(record[hdrLog], log)
	}

	// Various supercilious checks
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

func logDetails(fname string, log *zap.Logger) {

	var (
		err     error
		csvFile *os.File
	)

	if csvFile, err = os.Open(fname); err != nil {
		log.Error("Unable to open conversion log", zap.Error(err))
		return
	}
	defer csvFile.Close()

	const (
		hdrType        int = iota // "Type"
		hdrDescription            // "Description"
		hdrMax
	)

	enc, err := utils.DetectFileUTF(csvFile)
	if err != nil {
		log.Error("Unable to read conversion log", zap.Error(err))
	}

	r := csv.NewReader(utils.SelectReader(csvFile, enc))
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil {
		log.Error("unable to parse conversion log", zap.Error(err))
		return
	}
	if len(records) < 2 {
		// log is empty
		return
	}
	if len(records[0]) < hdrMax || !utils.EqualStringSlices([]string{"Type", "Description"}, records[0]) {
		log.Error("Unexpected conversion log header", zap.Strings("header", records[0]))
		return
	}

	for i := 1; i < len(records); i++ {
		if len(records[i]) < hdrMax {
			log.Error("Unexpected conversion log line", zap.Strings("line", records[i]))
			continue
		}
		switch t := records[i][hdrType]; {
		case strings.EqualFold("Warning", t):
			log.Warn(records[i][hdrDescription])
		case strings.EqualFold("Error", t):
			log.Error(records[i][hdrDescription])
		default:
			log.Info("KPV details", zap.String(t, records[i][hdrDescription]))
		}
	}
}
