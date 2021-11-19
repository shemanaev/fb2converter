package kfx

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/amzn/ion-go/ion"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"

	"fb2converter/archive"
	"fb2converter/config"
	// "fb2converter/utils"
)

// Default values.
const (
	defaultCompression     = 0
	defaultDRMScheme       = 0
	defaultChunkSize       = 4096
	defaultFragmentVersion = 1
)

type fragment struct {
	Version     int
	Compression int
	DRMScheme   int
	FType       ion.SymbolToken
	FID         ion.SymbolToken
	data        []byte
}

// func toBLOB(data []byte, ssts ...ion.SharedSymbolTable) ([]byte, error) {
// 	buf := new(bytes.Buffer)
// 	e := ion.NewBinaryEncoder(buf, ssts...)
// 	if err := e.EncodeAs(data, ion.BlobType); err != nil {
// 		return nil, err
// 	}
// 	if err := e.FinishNoLST(); err != nil {
// 		return nil, err
// 	}
// 	return buf.Bytes(), nil
// }

func newFragment(ftype, fid ion.SymbolToken, data []byte) (*fragment, error) {

	// reencode data as IonBLOB
	// if utils.IsOneOf(*ftype.Text, rawFragmentTypes) {
	// encode data as IonBLOB
	// var err error
	// if frag.data, err = toBLOB(data); err != nil {
	// 	return nil, err
	// }
	// }

	return &fragment{
		Version:     defaultFragmentVersion,
		Compression: defaultCompression,
		DRMScheme:   defaultDRMScheme,
		FID:         fid,
		FType:       ftype,
		data:        data,
	}, nil
}

// type parsed struct {
// 	symbols   ion.SymbolTable // $ion_symbol_table from KDF
// 	fragments []fragment      // book fragments
// }

// Packer - unpacks KPF/KDF and produces single KFX file for e-Ink devices.
type Transformer struct {
	log *zap.Logger
	// book *Container
}

// NewTransformer returns pointer to Packer with parsed KDF.
func NewTransformer(kpf, outDir string, log *zap.Logger) (*Transformer, error) {

	kdfDir := filepath.Join(outDir, config.DirKdf)
	if err := os.MkdirAll(kdfDir, 0700); err != nil {
		return nil, fmt.Errorf("unable to create directories for KDF contaner: %w", err)
	}
	// unwrapping KPF which is zipped KDF: content.kpf -> kdfDir
	if err := archive.Unzip(kpf, kdfDir); err != nil {
		return nil, fmt.Errorf("unable to unzip KDF contaner (%s): %w", kpf, err)
	}
	// book.kdf which is sqlite3 database is scrambled: kdf -> book.sqlite3
	kdfBook := filepath.Join(kdfDir, "resources", "book.kdf")
	sqlFile := filepath.Join(kdfDir, "book.sqlite")
	if err := unwrapSQLiteDB(kdfBook, sqlFile); err != nil {
		return nil, fmt.Errorf("unable to unwrap KDF contaner (%s): %w", kdfBook, err)
	}

	db, err := sql.Open("sqlite", sqlFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open sqlite3 database (%s): %w", sqlFile, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Warn("Unable to close database cleanly", zap.String("db", sqlFile), zap.Error(err))
		}
	}()

	tables, err := checkDBSchema(db)
	if err != nil {
		return nil, fmt.Errorf("bad book database schema, possibly new kindle previever was installed: %w", err)
	}

	eids, err := readKfxIDTranslations(db, tables, log)
	if err != nil {
		return nil, fmt.Errorf("unable to read KfxID translations: %w", err)
	}

	props, err := readFragmentProperties(db, tables, log)
	if err != nil {
		return nil, fmt.Errorf("unable to read fragment properties: %w", err)
	}

	frags, err := readFragments(db, eids, props, log)
	if err != nil {
		return nil, fmt.Errorf("unable to read fragments: %w", err)
	}
	println("FRAGS", fmt.Sprintf("%+v", frags))

	return &Transformer{
		log: log,
		// book: book,
	}, nil
}

func readFragments(db *sql.DB, eids map[int64]ion.SymbolToken, props map[string]string, log *zap.Logger) ([]*fragment, error) {

	frags := make([]*fragment, 0, 128)

	var ist []byte
	if err := db.QueryRow("SELECT payload_value FROM fragments WHERE id = '$ion_symbol_table' AND payload_type = 'blob';").Scan(&ist); err != nil {
		return frags, fmt.Errorf("unable to query $ion_symbol_table fragment: %w", err)
	}
	rdr := ion.NewReaderBytes(ist)
	if val, err := ion.NewDecoder(rdr).Decode(); err != nil && !errors.Is(err, ion.ErrNoInput) {
		return frags, fmt.Errorf("unable to decode KDF $ion_symbol_table fragment: %w", err)
	} else if val != nil {
		return frags, fmt.Errorf("unexpected value %+v for KDF $ion_symbol_table fragment ", val)
	}
	if len(rdr.SymbolTable().Imports()) != 2 {
		return frags, fmt.Errorf("unexpected number of imports %d for KDF $ion_symbol_table fragment: %s", len(rdr.SymbolTable().Imports()), rdr.SymbolTable().String())
	}
	if rdr.SymbolTable().Imports()[0].Name() != "$ion" || rdr.SymbolTable().Imports()[1].Name() != "YJ_symbols" {
		return frags, fmt.Errorf("unexpected import for KDF $ion_symbol_table fragment: %s", rdr.SymbolTable().String())
	}

	var (
		maxID uint64
		blob  []byte
	)
	if err := db.QueryRow("SELECT payload_value FROM fragments WHERE id = 'max_id' AND payload_type = 'blob';").Scan(&blob); err != nil {
		return frags, fmt.Errorf("unable to query max_id fragment: %w", err)
	}
	if err := ion.NewDecoder(ion.NewReaderBytes(blob)).DecodeTo(&maxID); err != nil {
		if !errors.Is(err, ion.ErrNoInput) {
			return frags, fmt.Errorf("unable to decode KDF max_id fragment: %w", err)
		}
		if maxID == 0 {
			return frags, errors.New("unexpected value in KDF max_id fragment: <nil>")
		}
	}
	if maxID != rdr.SymbolTable().MaxID() {
		return frags, fmt.Errorf("max_id (%d) in KDF max_id fragment is is not equial to number of symbols in KDF $ion_symbol_table fragment (%d)", maxID, rdr.SymbolTable().MaxID())
	}

	sstYJ := createSST(rdr.SymbolTable().Imports()[1].Name(), rdr.SymbolTable().Imports()[1].Version(), rdr.SymbolTable().Imports()[1].MaxID())
	stb := ion.NewSymbolTableBuilder(sstYJ)

	rows, err := db.Query("SELECT id, payload_type, payload_value FROM fragments WHERE id != 'max_id' and id != '$ion_symbol_table';")
	if err != nil {
		return frags, fmt.Errorf("unable to execute query: %w", err)
	}
	for rows.Next() {
		var id, ptype string
		if err := rows.Scan(&id, &ptype, &blob); err != nil {
			return frags, fmt.Errorf("unable to scan next row: %w", err)
		}
		switch ptype {
		case "blob":
			if len(blob) == 0 {
				ftype, ok := props[id]
				if !ok {
					ftype = "$0"
				}
				log.Debug("Empty KDF fragment (data is empty), ignoring...", zap.String("id", id), zap.String("type", ptype), zap.String("ftype", ftype))
				continue
			}
			if !bytes.HasPrefix(blob, ionBVM) {
				// Normally this does not happen - there is "path" record for that, but just in case...
				if !strings.HasPrefix(id, "resource/") {
					id = fmt.Sprintf("resource/%s", id)
				}
				frag, err := newFragment(createSymbolToken(stb, "$417", log), createSymbolToken(stb, id, log), blob)
				if err != nil {
					return frags, fmt.Errorf("unable to create path fragment id:(%s):payload_type(%s): %w", id, ptype, err)
				}
				frags = append(frags, frag)
				continue
			}

			if bytes.Equal(blob, ionBVM) {
				if id != "book_navigation" {
					log.Warn("Empty KDF fragment (BVM only), ignoring...", zap.String("id", id), zap.String("type", ptype))
				}
				continue
			}

			r := ion.NewReaderCat(io.MultiReader(bytes.NewReader(ist), bytes.NewReader(blob[len(ionBVM):])), ion.NewCatalog(sstYJ))
			if !r.Next() {
				if r.Err() != nil {
					return frags, fmt.Errorf("unable to read value annotations for KDF fragment %s: %w", id, r.Err())
				}
				return frags, fmt.Errorf("unable to read value annotations for KDF fragment %s: empty value", id)
			}
			annots, err := r.Annotations()
			if err != nil {
				return frags, fmt.Errorf("unable to read value annotations for KDF fragment %s: %w", id, err)
			}

			switch l := len(annots); {
			case l == 0:
				log.Error("KDF fragment must have annotation, skipping...", zap.String("id", id))
				continue
			case l == 2 && *annots[1].Text == "$608":
			case l > 1:
				log.Error("KDF fragment should have single annotation, ignoring...", zap.String("id", id), zap.Int("count", l))
				continue
			}
			if r.Type() == ion.NoType {
				log.Error("KDF fragment cannot be empty, ignoring...", zap.String("id", id))
				continue
			}
			data, err := dereferenceKfxIDs(r, stb, eids, log)
			if err != nil {
				return frags, fmt.Errorf("unable to dereference KDF fragment %s: %w", id, err)
			}
			frag, err := newFragment(createSymbolToken(stb, *annots[0].Text, log), createSymbolToken(stb, id, log), data)
			if err != nil {
				return frags, fmt.Errorf("unable to create dereferenced fragment id:(%s,%s):payload_type(%s): %w", *annots[0].Text, id, ptype, err)
			}
			frags = append(frags, frag)

		case "path":
			if !strings.HasPrefix(id, "resource/") {
				id = fmt.Sprintf("resource/%s", id)
			}
			frag, err := newFragment(createSymbolToken(stb, "$417", log), createSymbolToken(stb, id, log), blob)
			if err != nil {
				return frags, fmt.Errorf("unable to create path fragment id:(%s):payload_type(%s): %w", id, ptype, err)
			}
			frags = append(frags, frag)

		default:
			return frags, fmt.Errorf("unexpected KDF fragment type (%s) with id (%s) size %d", ptype, id, len(blob))
		}

	}
	if err := rows.Err(); err != nil {
		return frags, fmt.Errorf("unable to iterate on rows: %w", err)
	}
	return frags, nil
}

func readFragmentProperties(db *sql.DB, tables map[string]struct{}, _ *zap.Logger) (map[string]string, error) {

	props := make(map[string]string)
	// optional
	if _, found := tables["fragment_properties"]; !found {
		return props, nil
	}

	rows, err := db.Query("SELECT id, key, value FROM fragment_properties;")
	if err != nil {
		return props, fmt.Errorf("unable to execute query: %w", err)
	}
	for rows.Next() {
		var id, key, value string
		if err := rows.Scan(&id, &key, &value); err != nil {
			return props, fmt.Errorf("unable to scan next row: %w", err)
		}
		switch key {
		case "child":
		case "element_type":
			props[id] = value
		default:
			return props, fmt.Errorf("fragment property has unknown key: %s (%s:%s)", key, id, value)
		}
	}
	if err := rows.Err(); err != nil {
		return props, fmt.Errorf("unable to iterate on rows: %w", err)
	}
	return props, nil
}

func readKfxIDTranslations(db *sql.DB, tables map[string]struct{}, log *zap.Logger) (map[int64]ion.SymbolToken, error) {

	eids := make(map[int64]ion.SymbolToken)
	// optional
	if _, found := tables["kfxid_translation"]; !found {
		return eids, nil
	}

	rows, err := db.Query("SELECT eid, kfxid FROM kfxid_translation;")
	if err != nil {
		return eids, fmt.Errorf("unable to execute query: %w", err)
	}
	for rows.Next() {
		var (
			eid   int64
			kfxid string
		)
		if err := rows.Scan(&eid, &kfxid); err != nil {
			return eids, fmt.Errorf("unable to scan next row: %w", err)
		}
		eids[eid] = createLocalSymbolToken(kfxid, log)
	}
	if err := rows.Err(); err != nil {
		return eids, fmt.Errorf("unable to iterate on rows: %w", err)
	}
	return eids, nil
}

// checkDBSchema check book database schema sinse Amazon is know to change this at will.
// It makes sure that all needed tables exist and have proper structure and that book database does not have unexpected tables.
// On success it returns map of table names which were found in book database so calling code could decide what to do.
func checkDBSchema(db *sql.DB) (map[string]struct{}, error) {

	// those are the ones we know about
	var knowns = map[string]string{
		"CREATE TABLE index_info(namespace char(256), index_name char(256), property char(40), primary key (namespace, index_name)) without rowid": "index_info",
		"CREATE TABLE kfxid_translation(eid INTEGER, kfxid char(40), primary key(eid)) without rowid":                                              "kfxid_translation",
		"CREATE TABLE fragment_properties(id char(40), key char(40), value char(40), primary key (id, key, value)) without rowid":                  "fragment_properties",
		"CREATE TABLE fragments(id char(40), payload_type char(10), payload_value blob, primary key (id))":                                         "fragments",
		"CREATE TABLE gc_fragment_properties(id varchar(40), key varchar(40), value varchar(40), primary key (id, key, value)) without rowid":      "gc_fragment_properties",
		"CREATE TABLE gc_reachable(id varchar(40), primary key (id)) without rowid":                                                                "gc_reachable",
		"CREATE TABLE capabilities(key char(20), version smallint, primary key (key, version)) without rowid":                                      "capabilities",
	}

	var mustHave = map[string]struct{}{
		"capabilities": {},
		"fragments":    {},
	}

	names := make(map[string]struct{})

	rows, err := db.Query("SELECT name, sql FROM sqlite_master WHERE type='table';")
	if err != nil {
		return nil, fmt.Errorf("unable to get database tables: %w", err)
	}

	for rows.Next() {
		var tbl, schema string
		if err := rows.Scan(&tbl, &schema); err != nil {
			return nil, fmt.Errorf("unable to scan next row: %w", err)
		}
		if name, found := knowns[schema]; !found {
			return nil, fmt.Errorf("unexpected database table %s[%s]", tbl, schema)
		} else if name != tbl {
			return nil, fmt.Errorf("unexpected database table name %s for [%s]", tbl, schema)
		}
		if _, found := mustHave[tbl]; found {
			delete(mustHave, tbl)
		}
		names[tbl] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unable to iterate on rows: %w", err)
	}

	if len(mustHave) > 0 {
		var absent string
		for k := range mustHave {
			absent += " " + k
		}
		return nil, fmt.Errorf("unable to find some of expected tables:%s", absent)
	}
	return names, nil
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
