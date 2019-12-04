package kfx

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rupor-github/fb2converter/config"
)

// KDFTable enumerates supported tables in kdf container.
type KDFTable int

// Actual tables of interest.
// NOTE: index_info table is for dictionaries, currently not supported
// NOTE: gc_* tables are currently unused
const (
	TableSchema          KDFTable = iota // sqlite_master
	TableKFXID                           // kfxid_translation
	TableFragmentProps                   // fragment_properties
	TableFragments                       // fragments
	TableCapabilities                    // capabilities
	TableIndexInfo                       // index_info
	TableGCFragmentProps                 // gc_fragment_properties
	TableGCReachable                     // gc_reachable
	UnsupportedKDFTable                  //
)

// ParseKDFTableSring converts string to enum value. Case insensitive.
func ParseKDFTableSring(name string) KDFTable {

	for i := TableSchema; i < UnsupportedKDFTable; i++ {
		if strings.EqualFold(i.String(), name) {
			return i
		}
	}
	return UnsupportedKDFTable
}

// I do not want to use CGO - so I will use sqlite cli shell instead to dump tables.
func dumpKDFContainerContent(kpv *config.KPVEnv, dbfile, outDir string) error {

	tmpl := template.Must(template.New("query").Parse(`
{{range .}}
.mode quote
.output {{.}}.dat
{{if eq . "sqlite_master"}}
SELECT name FROM {{.}} WHERE type='table';
{{else}}
SELECT * FROM {{.}};
{{end}}
{{end}}
`))

	names := make([]string, 0, int(UnsupportedKDFTable))
	for i := TableSchema; i < UnsupportedKDFTable; i++ {
		names = append(names, i.String())
	}
	input := new(bytes.Buffer)
	err := tmpl.Execute(input, names)
	if err != nil {
		return err
	}
	return kpv.ExecSQL(bytes.NewBuffer(input.Bytes()), outDir, dbfile)
}

var numFields = []int{
	1, // TableSchema
	2, // TableKFXID
	3, // TableFragmentProps
	3, // TableFragments
	2, // TableCapabilities
}

// read sqlite table form dump file and parse information.
func readTable(table KDFTable, dir string, processRecord func(fields int, rec []string) (bool, error)) error {

	fname := filepath.Join(dir, table.String()+".dat")
	f, err := os.Open(fname)
	if err != nil {
		return fmt.Errorf("unable to open table [%s]: %w", table, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = numFields[table]

	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("unable to read table [%s]: %w", table, err)
		}
		if cont, err := processRecord(r.FieldsPerRecord, record); err != nil {
			return fmt.Errorf("unable to process record in the table [%s]: %w", table, err)
		} else if !cont {
			break
		}
	}
	return nil
}
