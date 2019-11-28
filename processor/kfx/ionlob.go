package kfx

import (
	"bytes"
)

// IonLob
//     IonBlob
//     IonClob

// ionClob represents Clob node.
type ionClob struct {
	node
	val []byte
}

func (*ionClob) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.clob
		return &ionClob{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionClob{}, nil
	}
	b, err := copyVarDataBytes(flag, r)
	if err != nil {
		return nil, err
	}
	return &ionClob{val: b}, nil
}

// ionBlob represents Blob node.
type ionBlob struct {
	node
	val []byte
}

func (*ionBlob) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.blob
		return &ionBlob{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionBlob{}, nil
	}
	b, err := copyVarDataBytes(flag, r)
	if err != nil {
		return nil, err
	}
	return &ionBlob{val: b}, nil
}
