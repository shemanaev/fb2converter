package kfx

import (
	"bytes"
)

// IonText
//     IonSymbol
//     IonString

// Symbol represents IonSymbol node.
type ionSymbol struct {
	node
	sid uint64 // for now not going to support unlimited number of symbols, this is not practical
	txt string
}

func (*ionSymbol) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.symbol
		return &ionSymbol{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionSymbol{}, nil
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}
	ux, err := readUInt64(vr)
	if err != nil {
		return nil, err
	}
	// TODO: whatever magic we may want to do here (look up symbol in tables, etc)
	return &ionSymbol{sid: ux}, nil
}

// String represents IonString node.
type ionString struct {
	node
	val string
}

func (*ionString) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.string
		return &ionString{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionString{}, nil
	}
	b, err := copyVarDataBytes(flag, r)
	if err != nil {
		return nil, err
	}
	return &ionString{val: string(b)}, nil
}
