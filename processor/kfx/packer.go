package kfx

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davecgh/go-spew/spew"
	"go.uber.org/zap"

	"github.com/rupor-github/fb2converter/archive"
	"github.com/rupor-github/fb2converter/config"
)

type parsed struct {
	translations map[uint64]string // symbols from kfxid_translation table
	elementType  map[string]string
}

// Packer - unpacks KPF/KDF and produces single file KFX for e-Ink devices.
type Packer struct {
	log  *zap.Logger
	book *parsed
}

// NewPacker returns pointer to Packer with parsed KDF.
// fname - path to intermediate KPF file.
func NewPacker(fname, outDir string, kpv *config.KPVEnv, log *zap.Logger) (*Packer, error) {

	kdfDir := filepath.Join(outDir, config.DirKdf)
	if err := os.MkdirAll(kdfDir, 0700); err != nil {
		return nil, fmt.Errorf("unable to create directories for KDF contaner: %w", err)
	}

	if err := unpackContainer(kpv, fname, kdfDir); err != nil {
		return nil, err
	}
	book, err := parseTables(kdfDir, log /*FIX ME*/)
	if err != nil {
		return nil, err
	}
	return &Packer{
		log:  log,
		book: book,
	}, nil
}

// unpackContainer unpacks KPF file and dumps KDF container tables.
func unpackContainer(kpv *config.KPVEnv, book, kdfDir string) error {

	if err := archive.Unzip(book, kdfDir); err != nil {
		return fmt.Errorf("unable to unzip KDF contaner: %w", err)
	}
	kdfBook := filepath.Join(kdfDir, "resources", "book.kdf")
	dbFile := filepath.Join(kdfDir, "book.sqlite3")
	if err := unwrapSQLiteDB(kdfBook, dbFile); err != nil {
		return fmt.Errorf("unable to unwrap KDF contaner: %w", err)
	}
	if err := dumpKDFContainerContent(kpv, dbFile, kdfDir); err != nil {
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

func unsq(in string) string {

	runeLen := utf8.RuneCountInString(in)
	if runeLen == 0 {
		return ""
	}

	runes := make([]rune, 0, runeLen)
	quote := false
	for _, r := range in {
		if quote || r != '\'' {
			runes = append(runes, r)
			quote = false
			continue
		}
		quote = true
	}
	return string(runes)
}

var (
	// ErrDRM returned when data in container has DRM.
	ErrDRM = errors.New("unable to decode blob: book container has DRM and cannot be converted")
	// DRM signature
	sigDRM = []byte("\xeaDRMION\xee")
)

func unsqBytes(in string) ([]byte, error) {

	runeLen := utf8.RuneCountInString(in)
	if runeLen <= 3 || !strings.HasPrefix(in, "X'") || !strings.HasSuffix(in, "'") {
		return nil, errors.New("not a blob")
	}

	runes := make([]rune, 0, runeLen)
	quote := false
	for _, r := range in[1:] {
		if quote || r != '\'' {
			runes = append(runes, r)
			quote = false
			continue
		}
		quote = true
	}
	res, err := hex.DecodeString(string(runes))
	if err != nil {
		return nil, fmt.Errorf("unable to decode blob: %w", err)
	}
	if bytes.HasPrefix(res, sigDRM) {
		return nil, ErrDRM
	}
	return res, nil
}

func parseTables(kdfDir string, log *zap.Logger) (*parsed, error) {

	p := &parsed{
		translations: make(map[uint64]string),
		elementType:  make(map[string]string),
	}

	// Parse KDF schema
	tables := make(map[KDFTable]bool)
	if err := readTable(TableSchema, kdfDir, func(max int, rec []string) (bool, error) {
		if t := ParseKDFTableSring(unsq(rec[0])); t != UnsupportedKDFTable {
			tables[t] = true
		} else {
			log.Debug("Found unknown KDF table", zap.String("name", rec[0]))
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	// Add symbols to book's local translation table
	if _, ok := tables[TableKFXID]; ok {
		if err := readTable(TableKFXID, kdfDir, func(max int, rec []string) (bool, error) {
			eid, err := strconv.ParseUint(unsq(rec[0]), 0, 64)
			if err != nil {
				return false, err
			}
			p.translations[eid] = unsq(rec[1])
			return true, nil
		}); err != nil {
			return nil, err
		}
	}

	if _, ok := tables[TableFragmentProps]; ok {
		if err := readTable(TableFragmentProps, kdfDir, func(_ int, rec []string) (bool, error) {
			switch unsq(rec[1]) {
			case "child":
			case "element_type":
				p.elementType[unsq(rec[0])] = unsq(rec[2])
			default:
				log.Warn("Fragment property has unknown key", zap.Strings("rec", rec))
			}
			return true, nil
		}); err != nil {
			return nil, err
		}
	}

	if _, ok := tables[TableFragments]; !ok {
		return nil, errors.New("KPF database is missing the 'fragments' table")
	}

	readValuesFromColumn := func(field string) ([]ionValue, error) {
		data, err := unsqBytes(field)
		if err != nil {
			return nil, err
		}
		vals, err := readValueStream(data)
		if err != nil {
			return nil, err
		}
		return vals, nil
	}

	// Build symbol tables
	var maxID ionValue
	var symData []ionValue
	if err := readTable(TableFragments, kdfDir, func(fields int, rec []string) (bool, error) {
		if fields < 3 {
			return false, fmt.Errorf("wrong number of fileds in table %s - %d", TableFragments, fields)
		}
		id, rtype := unsq(rec[0]), unsq(rec[1])
		if rtype != "blob" {
			return true, nil
		}
		switch id {
		case "$ion_symbol_table":
			vals, err := readValuesFromColumn(rec[2])
			if err != nil {
				return false, err
			}
			symData = vals
		case "max_id":
			vals, err := readValuesFromColumn(rec[2])
			if err != nil {
				return false, err
			}
			if len(vals) != 1 {
				return false, fmt.Errorf("multiple max_id values in a stream (%d)", len(vals))
			}
			maxID = vals[0]
		}
		return !(maxID != nil && symData != nil), nil
	}); err != nil {
		return nil, err
	}
	println("AAAAAAAAAA", spew.Sdump(symData), spew.Sdump(maxID))

	// Read fragments
	if err := readTable(TableFragments, kdfDir, func(_ int, rec []string) (bool, error) {
		if unsq(rec[0]) == "$ion_symbol_table" {
			data, err := unsqBytes(rec[2])
			if err != nil {
				return false, err
			}
			log.Debug("SYMTABLE found", zap.String("id", unsq(rec[0])), zap.String("type", unsq(rec[1])), zap.Int("len", len(data)), zap.String("table", string(data)))
		}
		if unsq(rec[0]) == "max_id" {
			data, err := unsqBytes(rec[2])
			if err != nil {
				return false, err
			}
			log.Debug("MAX_ID found", zap.String("id", unsq(rec[0])), zap.String("type", unsq(rec[1])), zap.Int("len", len(data)), zap.String("data", string(data)))
		}
		// data, err := unsqBytes(rec[2])
		// if err != nil {
		// 	return err
		// }
		// log.Debug("FRAG found", zap.String("id", unsq(rec[0])), zap.String("type", unsq(rec[1])), zap.Int("len", len(data)))
		return true, nil
	}); err != nil {
		return nil, err
	}
	return p, nil
}
