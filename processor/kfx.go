package processor

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/rupor-github/fb2converter/archive"
	"github.com/rupor-github/fb2converter/config"
	"github.com/rupor-github/fb2converter/utils"
)

type parsedBook struct {
}

// FinalizeKFX produces final KFX file out of previously saved temporary files.
func (p *Processor) FinalizeKFX(fname string) error {

	_, err := p.generateIntermediateKPFContent(fname)
	if err != nil {
		return fmt.Errorf("unable to generate intermediate content: %w", err)
	}
	return fmt.Errorf("FIX ME DONE: %s", fname)
}

// generateIntermediateKPFContent produces temporary KPF file, presently by running Kindle Previewer and returns its full path.
func (p *Processor) generateIntermediateKPFContent(fname string) (*parsedBook, error) {

	outDir := filepath.Join(p.tmpDir, DirKfx)
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return nil, fmt.Errorf("unable to create data directories for Kindle Previewer: %w", err)
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
		return nil, err
	}
	book, err := checkResults(outDir, p.env.Log)
	if err != nil {
		return nil, err
	}
	if err := unpackContainer(p.kpvEnv, book, outDir); err != nil {
		return nil, err
	}

	return parseTables(outDir)
}

func parseTables(outDir string) (*parsedBook, error) {
	return nil, nil
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

// unpackContainer unpacks KPF file and dumps KDF container tables.
func unpackContainer(kpv *config.KPVEnv, book, outDir string) error {

	kdfDir := filepath.Join(outDir, DirKdf)
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return fmt.Errorf("unable to create directories for KDF contaner: %w", err)
	}
	if err := archive.Unzip(book, kdfDir); err != nil {
		return fmt.Errorf("unable to unzip KDF contaner: %w", err)
	}
	kdfBook := filepath.Join(kdfDir, "resources", "book.kdf")
	dbFile := filepath.Join(outDir, "book.sqlite3")
	if err := unwrapSQLiteDB(kdfBook, dbFile); err != nil {
		return fmt.Errorf("unable to unwrap KDF contaner: %w", err)
	}
	if err := dumpKDFContainerContent(kpv, dbFile, outDir); err != nil {
		return fmt.Errorf("unable to dump KDF tables: %w", err)
	}
	return nil
}

// unwrapSQLiteDB restores proper SQLite DB out of KDF file.
func unwrapSQLiteDB(from, to string) error {

	const (
		wrapperOffset      = 0x400
		wrapperLength      = 0x400
		wrapperFrameLength = 0x100000
	)

	var (
		data        []byte
		err         error
		signature   = []byte("SQLite format 3\x00")
		fingerprint = []byte("\xfa\x50\x0a\x5f")
		header      = []byte("\x01\x00\x00\x40\x20")
	)

	if data, err = ioutil.ReadFile(from); err != nil {
		return err
	}
	if len(data) <= len(signature) || len(data) < 2*wrapperOffset {
		return fmt.Errorf("unexpected SQLite file length: %d", len(data))
	}
	if !bytes.Equal(signature, data[:len(signature)]) {
		return fmt.Errorf("unexpected SQLite file signature: %v", data[:len(signature)])
	}

	unwrapped := make([]byte, 0, len(data))
	prev, curr := 0, wrapperOffset
	for ; curr+wrapperLength <= len(data); prev, curr = curr+wrapperLength, curr+wrapperLength+wrapperFrameLength {
		if !bytes.Equal(fingerprint, data[curr:curr+len(fingerprint)]) {
			return fmt.Errorf("unexpected fingerprint: %v", data[curr:curr+len(fingerprint)])
		}
		if !bytes.Equal(header, data[curr+len(fingerprint):curr+len(fingerprint)+len(header)]) {
			return fmt.Errorf("unexpected fingerprint header: %v", data[curr+len(fingerprint):curr+len(fingerprint)+len(header)])
		}
		unwrapped = append(unwrapped, data[prev:curr]...)
	}
	unwrapped = append(unwrapped, data[prev:]...)

	if err = ioutil.WriteFile(to, unwrapped, 0644); err != nil {
		return err
	}
	return nil
}
