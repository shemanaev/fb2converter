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
	"github.com/rupor-github/ion-go"
	"go.uber.org/zap"

	"github.com/rupor-github/fb2converter/archive"
	"github.com/rupor-github/fb2converter/config"
)

type parsed struct {
	symbols      ion.SymbolTable   // $ion_symbol_table from KDF
	translations map[uint64]string // symbols from kfxid_translation table
}

// Packer - unpacks KPF/KDF and produces single file KFX for e-Ink devices.
type Packer struct {
	log  *zap.Logger
	book *parsed
}

// NewPacker returns pointer to Packer with parsed KDF.
// fname - path to intermediate KPF file.
func NewPacker(fname, outDir string, kpv *config.KPVEnv, debug bool, log *zap.Logger) (*Packer, error) {

	kdfDir := filepath.Join(outDir, config.DirKdf)
	if err := os.MkdirAll(kdfDir, 0700); err != nil {
		return nil, fmt.Errorf("unable to create directories for KDF contaner: %w", err)
	}

	if err := unpackContainer(kpv, fname, kdfDir); err != nil {
		return nil, err
	}
	book, err := parseTables(kdfDir, debug, log)
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
	if runeLen < 3 || !strings.HasPrefix(in, "X'") || !strings.HasSuffix(in, "'") {
		return nil, errors.New("not a blob")
	}

	// empty blob
	if runeLen == 3 {
		return []byte{}, nil
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

func parseTables(kdfDir string, debug bool, log *zap.Logger) (*parsed, error) {

	var kdfDump *os.File
	if debug {
		var err error
		if kdfDump, err = os.Create(filepath.Join(kdfDir, "kdf-dump.txt")); err != nil {
			return nil, fmt.Errorf("unable to create kdf-dump file: %w", err)
		}
		defer kdfDump.Close()
	}

	p := &parsed{
		translations: make(map[uint64]string),
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

	if kdfDump != nil {
		kdfDump.WriteString(fmt.Sprintf("-------> %s\n%s\n\n", TableKFXID, spew.Sdump(p.translations)))
	}

	elTypes := make(map[string]string)

	// Get fragment properties
	if _, ok := tables[TableFragmentProps]; ok {
		if err := readTable(TableFragmentProps, kdfDir, func(_ int, rec []string) (bool, error) {
			switch unsq(rec[1]) {
			case "child":
			case "element_type":
				elTypes[unsq(rec[0])] = unsq(rec[2])
			default:
				log.Warn("Fragment property has unknown key", zap.Strings("rec", rec))
			}
			return true, nil
		}); err != nil {
			return nil, err
		}
	}

	if kdfDump != nil {
		kdfDump.WriteString(fmt.Sprintf("-------> %s\n%s\n\n", TableFragmentProps, spew.Sdump(elTypes)))
	}

	// See if we have actual fragments to parse
	if _, ok := tables[TableFragments]; !ok {
		return nil, errors.New("KPF database is missing the 'fragments' table")
	}

	// read and decode data
	type record struct {
		id, rtype string
		data      []byte
	}

	var (
		maxIDRec       record
		ionSymTableRec record
		records        = make([]record, 0, 1024)
	)

	// Read fragments from database
	if err := readTable(TableFragments, kdfDir, func(fields int, rec []string) (bool, error) {

		if fields < 3 {
			return false, fmt.Errorf("wrong number of fileds in table %s - %d", TableFragments, fields)
		}

		r := record{
			id:    unsq(rec[0]),
			rtype: unsq(rec[1]),
		}

		if data, err := unsqBytes(rec[2]); err != nil {
			return false, fmt.Errorf("unable to read fragment from KDF: %w", err)
		} else if len(data) == 0 {
			log.Debug("Empty KDF fragment, ignoring...", zap.String("id", r.id), zap.String("type", r.rtype))
			return true, nil
		} else {
			r.data = data
		}

		switch r.id {
		case "$ion_symbol_table":
			ionSymTableRec = r
		case "max_id":
			maxIDRec = r
		default:
			records = append(records, r)
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	if len(maxIDRec.id) == 0 {
		return nil, errors.New("unable to find max_id in KDF")
	}
	if len(ionSymTableRec.id) == 0 {
		return nil, errors.New("unable to find $ion_symbol_table in KDF")
	}

	cat := ion.NewCatalog(yjSymbolTable)
	rdr := ion.NewReaderCat(bytes.NewReader(ionSymTableRec.data), cat)
	dec := ion.NewDecoder(rdr)

	// symbol table
	val, err := dec.Decode()
	if err != nil && !errors.Is(err, ion.ErrNoInput) {
		return nil, fmt.Errorf("unable to decode KDF $ion_symbol_table: %w", err)
	}
	if val != nil {
		return nil, fmt.Errorf("unexpected value in KDF $ion_symbold_table fragment: %+v", val)
	}
	// store it for later
	p.symbols = rdr.SymbolTable()

	// validate number of symbols before continuing
	dec = ion.NewDecoder(ion.NewReader(bytes.NewReader(maxIDRec.data)))
	val, err = dec.Decode()
	if err != nil && !errors.Is(err, ion.ErrNoInput) {
		return nil, fmt.Errorf("unable to decode KDF max_id: %w", err)
	}
	if val == nil {
		return nil, errors.New("unexpected value in KDF max_id fragment: <nil>")
	}
	if maxID, ok := val.(int); !ok {
		return nil, fmt.Errorf("max_id in KDF is not integer (%T)", val)
	} else if uint64(maxID) != p.symbols.MaxID() {
		return nil, fmt.Errorf("max_id in KDF fragment (%d) is is not equial to number of symbols (%d)", maxID, p.symbols.MaxID())
	}

	if kdfDump != nil {
		kdfDump.WriteString(fmt.Sprintf("-------> %s\n$ion_symbol_table: %s, max_id: %d\n\n", TableFragments, p.symbols.String(), p.symbols.MaxID()))
	}

loop:
	for _, rec := range records {
		switch rec.rtype {
		case "blob":
			if rec.id == "max_eid_in_sections" {
				log.Warn("unexpected max_eid_in_sections for non-dictionary, ignoring...")
				continue loop
			}
			if !bytes.HasPrefix(rec.data, ionBVM) {
				// this should never happen - let's assume this is resource
				if kdfDump != nil {
					kdfDump.WriteString(fmt.Sprintf("PATH rid=%s\t==>\t\"%s\"\n", rec.id, string(rec.data)))
				}
				continue
			}
			if bytes.Equal(rec.data, ionBVM) {
				log.Debug("Empty KDF fragment (BVM only), ignoring...", zap.String("id", rec.id), zap.String("type", rec.rtype))
				continue
			}
			// dec = ion.NewDecoder(ion.NewReader(bytes.NewReader(rec.data)))
			// val, err = dec.Decode()
			// if err != nil && !errors.Is(err, ion.ErrNoInput) {
			// 	return nil, fmt.Errorf("unable to decode KDF fragment #%d (%s): %w", i, rec.id, err)
			// }
			if kdfDump != nil {
				kdfDump.WriteString(fmt.Sprintf("BLOB rid=%s\t==>\t%s\n", rec.id, dumpToText(rec.data)))
			}
		case "path":
			if kdfDump != nil {
				kdfDump.WriteString(fmt.Sprintf("PATH rid=%s\t==>\t\"%s\"\n", rec.id, string(rec.data)))
			}
		default:
			return nil, fmt.Errorf("unexpected KDF fragment type (%s) with id (%s) size %d", rec.rtype, rec.id, len(rec.data))
		}
	}
	return p, nil
}
