package kfx

import (
	"fmt"

	"github.com/amzn/ion-go/ion"
)

// Entity represents content fragment.
type Entity struct {
	// cntnr       *Container
	Version     int
	Compression int
	DRMScheme   int
	FType       ion.SymbolToken
	FID         ion.SymbolToken
	data        []byte
}

func (e *Entity) String() string {
	return fmt.Sprintf(
		"ver: %2d, comp: %2d, drm: %2d, ftype: <%s|%d>, fid: <%s|%d>",
		e.Version,
		e.Compression,
		e.DRMScheme,
		*e.FType.Text, e.FType.LocalSID,
		*e.FID.Text, e.FID.LocalSID,
	)
}
