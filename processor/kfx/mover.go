package kfx

import (
	"bytes"
	// "errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	// "strconv"
	// "strings"

	"github.com/amzn/ion-go/ion"
	"go.uber.org/zap"

	"github.com/rupor-github/fb2converter/archive"
	"github.com/rupor-github/fb2converter/config"
	// "github.com/rupor-github/fb2converter/utils"
)

type fragment struct {
	ftype string
	value interface{}
}

type parsed struct {
	symbols   ion.SymbolTable // $ion_symbol_table from KDF
	fragments []fragment      // book fragments
}

// Packer - unpacks KPF/KDF and produces single file KFX for e-Ink devices.
type Mover struct {
	log *zap.Logger
	// book *Container
}

// NewMover returns pointer to Packer with parsed KDF.
func NewMover(kpf, outDir string, log *zap.Logger) (*Mover, error) {

	kdfDir := filepath.Join(outDir, config.DirKdf)
	if err := os.MkdirAll(kdfDir, 0700); err != nil {
		return nil, fmt.Errorf("unable to create directories for KDF contaner: %w", err)
	}
	// unwrapping KPF which is just zipped KDF
	if err := unpackKPFContainer(kpf, kdfDir); err != nil {
		return nil, fmt.Errorf("unable to prepare KDF data: %s", err)
	}
	/*
		book, err := parseTables(kdfDir, debug, log)
		if err != nil {
			return nil, fmt.Errorf("unable to parse KDF data: %w", err)
		}
		if err := book.convertData(); err != nil {
			return nil, fmt.Errorf("unable to convert KDF data to KFX: %w", err)
		}

		// FIXME
		book.Dump(os.Stderr)
	*/
	return &Mover{
		log: log,
		// book: book,
	}, nil
}

// unpackContainer unpacks KPF file and dumps KDF container tables.
func unpackKPFContainer(kpf, kdfDir string) error {

	if err := archive.Unzip(kpf, kdfDir); err != nil {
		return fmt.Errorf("unable to unzip KDF contaner: %w", err)
	}
	kdfBook := filepath.Join(kdfDir, "resources", "book.kdf")
	dbFile := filepath.Join(kdfDir, "book.sqlite3")
	// internally sqlite3 database of ion symbols is scrambled
	if err := unwrapSQLiteDB(kdfBook, dbFile); err != nil {
		return fmt.Errorf("unable to unwrap KDF contaner: %w", err)
	}
	/*
		if err := dumpKDFContainerContent(dbFile, kdfDir); err != nil {
			return fmt.Errorf("unable to dump KDF tables: %w", err)
		} */
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

	if err = ioutil.WriteFile(to, unwrapped, 0600); err != nil {
		return err
	}
	return nil
}

/*
func parseTables(kdfDir string, debug bool, log *zap.Logger) (*Container, error) {

	dmp, err := utils.NewDumper(filepath.Join(kdfDir, "kdf-dump.txt"), debug)
	if err != nil {
		return nil, fmt.Errorf("unable to create kdf-dump file: %w", err)
	}
	defer dmp.Close()

	cntnr := NewContainer(log)
	stb := ion.NewSymbolTableBuilder(cntnr.YJSymbolTable)

	dmp.FmtWrite("-------> %s\n", TableSchema)

	// Parse KDF schema
	tables := make(map[KDFTable]bool)
	if err := readTable(TableSchema, kdfDir, func(max int, rec []string) (bool, error) {
		if t := ParseKDFTableSring(unsq(rec[0])); t != UnsupportedKDFTable {
			dmp.FmtWrite("TABLE %s\n", t)
			tables[t] = true
		} else {
			log.Debug("Found unknown KDF table", zap.String("name", rec[0]))
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	dmp.FmtWrite("\n-------> %s\n", TableKFXID)

	// Add symbols to book's local translation table (only for dictionaries?)
	eid2sym := make(map[int64]*ion.SymbolToken)

	if _, ok := tables[TableKFXID]; ok {
		if err := readTable(TableKFXID, kdfDir, func(max int, rec []string) (bool, error) {
			eid, err := strconv.ParseInt(unsq(rec[0]), 10, 64)
			if err != nil {
				return false, err
			}
			kfxID := unsq(rec[1])
			dmp.FmtWrite("EID %d\t==>%s\n", eid, kfxID)
			eid2sym[eid] = addLocalSymbol(stb, kfxID)
			return true, nil
		}); err != nil {
			return nil, err
		}
	}

	dmp.FmtWrite("\n-------> %s\n", TableFragmentProps)

	// Get fragment types (rupor: have not found use for it yet)
	elTypes := make(map[string]string)

	// Get fragment properties
	if _, ok := tables[TableFragmentProps]; ok {
		if err := readTable(TableFragmentProps, kdfDir, func(_ int, rec []string) (bool, error) {
			switch unsq(rec[1]) {
			case "child":
			case "element_type":
				key, value := rec[0], rec[2]
				elTypes[key] = value
				dmp.FmtWrite("PROP %s\t ==> %s\n", key, value)
			default:
				log.Warn("Fragment property has unknown key", zap.Strings("rec", rec))
			}
			return true, nil
		}); err != nil {
			return nil, err
		}
	}

	// See if we have actual fragments to parse
	if _, ok := tables[TableFragments]; !ok {
		return nil, errors.New("KPF database is missing the 'fragments' table")
	}

	// read and decode actual data
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

		r := record{id: unsq(rec[0]), rtype: unsq(rec[1])}
		if data, err := unsqBytes(rec[2]); err != nil {
			return false, fmt.Errorf("unable to read fragment from KDF: %w", err)
		} else {
			r.data = data
		}

		if !utils.IsOneOf(r.rtype, []string{"blob", "path"}) {
			log.Debug("Ignoring KDF fragment", zap.String("id", r.id), zap.String("type", r.rtype))
			return true, nil
		}

		switch r.id {
		case "$ion_symbol_table":
			ionSymTableRec = r
		case "max_id":
			maxIDRec = r
		case "max_eid_in_sections":
			log.Warn("Unexpected max_eid_in_sections for non-dictionary, ignoring...", zap.String("type", r.rtype))
		default:
			records = append(records, r)
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	// Check if we have symbol data
	if len(maxIDRec.id) == 0 {
		return nil, errors.New("unable to find max_id in KDF")
	}
	if len(ionSymTableRec.id) == 0 {
		return nil, errors.New("unable to find $ion_symbol_table in KDF")
	}

	// Reconstruct LST from KDF
	rdr := ion.NewReader(bytes.NewReader(ionSymTableRec.data))
	val, err := ion.NewDecoder(rdr).Decode()
	if err != nil && !errors.Is(err, ion.ErrNoInput) {
		return nil, fmt.Errorf("unable to decode KDF $ion_symbol_table: %w", err)
	}
	if val != nil {
		return nil, fmt.Errorf("unexpected value %+v for document symbols in KDF container", val)
	}
	imps := rdr.SymbolTable().Imports()
	if len(imps) != 2 {
		return nil, fmt.Errorf("unexpected number of imports %d for document symbols in KDF container: %s", len(imps), rdr.SymbolTable().String())
	}
	if imps[0].Name() != "$ion" || imps[1].Name() != "YJ_symbols" {
		return nil, fmt.Errorf("unexpected import for document symbols in KDF container: %s", rdr.SymbolTable().String())
	}
	cntnr.SymbolData = ionSymTableRec.data
	cntnr.YJSymbolTable = createYJSymbolTable(imps[1].MaxID())
	cntnr.symbols = rdr.SymbolTable()

	// Validate number of symbols
	val, err = ion.NewDecoder(ion.NewReader(bytes.NewReader(maxIDRec.data))).Decode()
	if err != nil && !errors.Is(err, ion.ErrNoInput) {
		return nil, fmt.Errorf("unable to decode KDF max_id: %w", err)
	}
	if val == nil {
		return nil, errors.New("unexpected value in KDF max_id fragment: <nil>")
	}
	if maxID, ok := val.(int); !ok {
		return nil, fmt.Errorf("max_id in KDF is not integer (%T)", val)

	} else if uint64(maxID) != cntnr.symbols.MaxID() {
		return nil, fmt.Errorf("max_id in KDF fragment (%d) is is not equial to number of symbols (%d)", maxID, cntnr.symbols.MaxID())
	}

	dmp.FmtWrite("\n-------> %s\nmax_id: %d, $ion_symbol_table: %s\n\n", TableFragments, cntnr.symbols.MaxID(), cntnr.symbols.String())

	for _, rec := range records {
		switch rec.rtype {
		case "blob":

			if len(rec.data) == 0 {
				ftype, ok := elTypes[rec.id]
				if !ok {
					ftype = "$0"
				}
				log.Debug("Empty KDF fragment (data is empty), ignoring...", zap.String("id", rec.id), zap.String("type", rec.rtype), zap.String("ftype", ftype))
				continue
			}

			if !bytes.HasPrefix(rec.data, ionBVM) {
				dmp.FmtWrite("BLOB(PATH) rid=%s\t==>\t\"%s\"\n", rec.id, string(rec.data))

				// Normally this does not happen - there is "PATH" record for that
				if !strings.HasPrefix(rec.id, "resource/") {
					rec.id = fmt.Sprintf("resource/%s", rec.id)
				}
				e, err := cntnr.NewEntity(addLocalSymbol(stb, "$417"), addLocalSymbol(stb, rec.id), rec.data)
				if err != nil {
					return nil, err
				}
				cntnr.Entities = append(cntnr.Entities, e)
				continue
			}

			if bytes.Equal(rec.data, ionBVM) {
				if rec.id != "book_navigation" {
					log.Warn("Empty KDF fragment (BVM only), ignoring...", zap.String("id", rec.id), zap.String("type", rec.rtype))
				}
				continue
			}

			dmp.FmtWrite("BLOB rid=%s\t==>\t%s\n", rec.id, dumpBytes(cntnr.createBytesWithLST(rec.data)))

			haveVal, annots, err := cntnr.readAnnotations(rec.data)
			if err != nil {
				return nil, err
			}
			switch l := len(annots); {
			case l == 0:
				log.Error("KDF fragment must have annotation, skipping...", zap.String("id", rec.id))
				continue
			case l == 2 && *annots[1].Text == "$608":
			case l > 1:
				log.Error("KDF fragment should have single annotation, ignoring...", zap.String("id", rec.id), zap.Int("count", l))
				continue
			}
			if !haveVal {
				log.Error("KDF fragment cannot be empty, ignoring...", zap.String("id", rec.id))
				continue
			}
			data, err := cntnr.derefIDs(rec.data, stb, eid2sym)
			if err != nil {
				return nil, err
			}
			e, err := cntnr.NewEntity(addLocalSymbol(stb, *annots[0].Text), addLocalSymbol(stb, rec.id), data)
			if err != nil {
				return nil, err
			}
			cntnr.Entities = append(cntnr.Entities, e)

		case "path":
			dmp.FmtWrite("PATH rid=%s\t==>\t\"%s\"\n", rec.id, string(rec.data))

			if !strings.HasPrefix(rec.id, "resource/") {
				rec.id = fmt.Sprintf("resource/%s", rec.id)
			}
			e, err := cntnr.NewEntity(addLocalSymbol(stb, "$417"), addLocalSymbol(stb, rec.id), rec.data)
			if err != nil {
				return nil, err
			}
			cntnr.Entities = append(cntnr.Entities, e)

		default:
			return nil, fmt.Errorf("unexpected KDF fragment type (%s) with id (%s) size %d", rec.rtype, rec.id, len(rec.data))
		}
	}

	// save symbol table we just built
	cntnr.symbols = stb.Build()
	cntnr.YJSymbolTable = cntnr.symbols.Imports()[1]
	buf := new(bytes.Buffer)
	w := ion.NewBinaryWriter(buf)
	if err := cntnr.symbols.WriteTo(w); err != nil {
		return nil, err
	}
	if err := w.Finish(); err != nil {
		return nil, err
	}
	cntnr.SymbolData = buf.Bytes()

	dmp.FmtWrite("\n-------> %s\n", TableCapabilities)

	// Get format capabilities
	caps := make([]map[string]interface{}, 0, 16)
	if err := readTable(TableCapabilities, kdfDir, func(fields int, rec []string) (bool, error) {
		ver, err := strconv.Atoi(rec[1])
		if err != nil {
			log.Error("Unable to get capability version, ignoring...", zap.String("key", rec[0]), zap.String("ver", rec[1]), zap.Error(err))
			return true, nil
		}
		dmp.FmtWrite("CAPABILITY %s\t==>\t%d\n", unsq(rec[0]), ver)
		caps = append(caps, map[string]interface{}{"$492": rec[0], "version": ver})
		return true, nil
	}); err != nil {
		return nil, err
	}

	// p.fragments = append(p.fragments, fragment{ftype: "$593", value: map[string]interface{}{"$492": rec[0], "version": ver}})

	// p.fragments = append(p.fragments, fragment{ftype: "$270", value: map[string]interface{}{"$587": "", "$588": "", "$161": "KPF"}})

	// FIXME: additional metadata?

	// dmp.FmtWrite("\n-------> fragments %d\n", len(p.fragments))
	// for i, f := range p.fragments {
	// 	dmp.FmtWrite("\n%d -------> %+v", i, f)
	// }

	return cntnr, nil
}
*/
