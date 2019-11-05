package kfx

import (
	"strings"
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
