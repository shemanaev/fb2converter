package kfx

import (
	// "bytes"
	// "crypto/sha1"
	// "encoding/binary"
	// "encoding/hex"
	// "errors"
	"fmt"
	// "io"
	// "io/ioutil"
	// "math"
	"strconv"
	// "strings"

	"github.com/amzn/ion-go/ion"
	"go.uber.org/zap"
	// "fb2converter/utils"
)

// Magic numbers.
var (
	ContainerSignature = [4]byte{'C', 'O', 'N', 'T'}
	ContainerVersions  = map[uint16]bool{1: true, 2: true}

	EntitySignature = [4]byte{'E', 'N', 'T', 'Y'}
	EntityVersions  = map[uint16]bool{1: true}
)

// Default values.
const (
	DefaultCompression      = 0
	DefaultDRMScheme        = 0
	DefaultChunkSize        = 4096
	DefaultContainerVersion = 2
	DefaultEntityVersion    = 1
)

// Capabilities keep format capabilities. Honestly I do not know what to do with thenm yet, so let's keep it simple for now.
type Capabilities []byte

func (c Capabilities) FullString(cntnr *Container) string {
	return "AAAAAAAAAAAA"
	// 	return dumpBytes(cntnr.createBytesWithLST([]byte(c)))
}

// Container - mighty KFX itself, logical representation.
type Container struct {
	log           *zap.Logger
	ACR           string
	Compression   int
	DRMScheme     int
	ChunkSize     int
	Version       int
	AppVersion    string
	PkgVersion    string
	Caps          Capabilities
	Digest        string
	Entities      []*Entity
	YJSymbolTable ion.SharedSymbolTable // appropriately sized YJ_symbols table
	SymbolData    []byte                // raw LST data
	symbols       ion.SymbolTable       // parsed LST
}

func (cntnr *Container) String() string {
	return fmt.Sprintf(
		"ACR: %s, ver: %d, comp: %2d, drm: %2d, chunk: %5d, app: %s, pkg: %s, caps: %s, sha1: \"%s\", symbols/max_id: %d/%d, entities: %d",
		cntnr.ACR,
		cntnr.Version,
		cntnr.Compression,
		cntnr.DRMScheme,
		cntnr.ChunkSize,
		strconv.Quote(cntnr.AppVersion),
		strconv.Quote(cntnr.PkgVersion),
		cntnr.Caps.FullString(cntnr),
		cntnr.Digest,
		len(cntnr.symbols.Symbols()), cntnr.symbols.MaxID(),
		len(cntnr.Entities),
	)
}
