package kfx

import (
	"bytes"
	"errors"
	"math/big"
)

// TypeAnnotations represent value's user type annotation.
type typeAnnotations map[uint64]struct{}

// Add adds a user type annotation to the annotations attached to value.
func (a typeAnnotations) add(name uint64) {
	var emptyStruct struct{}
	a[name] = emptyStruct
}

// Del removes a user type annotation from the list of annotations attached to value.
func (a typeAnnotations) del(name uint64) {
	delete(a, name)
}

// Has determines whether or not the value is annotated with a particular user type annotation.
func (a typeAnnotations) has(name uint64) bool {
	_, exists := a[name]
	return exists
}

// Clear removes all the user type annotations attached to value.
func (a typeAnnotations) clear() {
	for k := range a {
		delete(a, k)
	}
}

type ionValue interface {
	// is node ion null value, e.g., null or null.type
	isNull() bool
	// node type annotations
	getTA() typeAnnotations
	setTA(a typeAnnotations)
}

type node struct {
	null bool
	ta   typeAnnotations
}

func (n *node) isNull() bool {
	return n.null
}

func (n *node) getTA() typeAnnotations {
	return n.ta
}

func (n *node) setTA(ta typeAnnotations) {
	if ta == nil {
		// create empty map
		ta = make(typeAnnotations)
	}
	n.ta = ta
}

// Ion binary stream types.

type valueCode byte

const (
	vcNull       valueCode = iota // null
	vcBool                        // bool
	vcPInt                        // int
	vcNInt                        // int
	vcFloat                       // float
	vcDecimal                     // decimal
	vcTimestamp                   // timestamp
	vcSymbol                      // symbol
	vcString                      // string
	vcCLOB                        // clob
	vcBLOB                        // blob
	vcList                        // list
	vcSExp                        // sexp
	vcStruct                      // struct
	vcAnnotation                  // annotation
	vcReserved                    // reserved
)

func (c valueCode) read(flag uint64, r *bytes.Reader) (ionValue, error) {
	switch c {
	case vcNull:
		return (*ionNull).fromBytes(nil, flag, r)
	case vcBool:
		return (*ionBool).fromBytes(nil, flag, r)
	case vcPInt:
		i := &ionInt{val: big.NewInt(1)}
		return i.fromBytes(flag, r)
	case vcNInt:
		i := &ionInt{val: big.NewInt(-1)}
		return i.fromBytes(flag, r)
	case vcFloat:
		return (*ionFloat).fromBytes(nil, flag, r)
	case vcDecimal:
		return (*ionDecimal).fromBytes(nil, flag, r)
	case vcTimestamp:
		return (*ionTimestamp).fromBytes(nil, flag, r)
	case vcSymbol:
		return (*ionSymbol).fromBytes(nil, flag, r)
	case vcString:
		return (*ionString).fromBytes(nil, flag, r)
	case vcCLOB:
		return (*ionClob).fromBytes(nil, flag, r)
	case vcBLOB:
		return (*ionBlob).fromBytes(nil, flag, r)
	case vcList:
		return (*ionList).fromBytes(nil, flag, r)
	case vcSExp:
		return (*ionSexp).fromBytes(nil, flag, r)
	case vcStruct:
		return (*ionStruct).fromBytes(nil, flag, r)
	case vcAnnotation:
		return readAnnotatedValue(flag, r)
	case vcReserved:
		fallthrough
	default:
		return nil, errors.New("reserved value type")
	}
}
