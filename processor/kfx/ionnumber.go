package kfx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/cockroachdb/apd/v2"
)

// IonNumber
//     IonInt
//     IonFloat
//     IonDecimal

type ionInt struct {
	node
	val *big.Int
}

func (pi *ionInt) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.int
		pi.val = nil
		pi.null = true
		return pi, nil
	}

	neg := pi.val.Sign() < 0
	if flag == 0 {
		if !neg {
			// type 2 positive integer or zero
			pi.val = big.NewInt(0)
			return pi, nil
		}
		// type 3 negative integer
		return nil, errors.New("negative integer cannot be zero")
	}

	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}
	pi.val, err = readBigUInt(vr)
	if err != nil {
		return nil, err
	}
	if neg {
		pi.val.Neg(pi.val)
	}
	return pi, nil
}

type ionFloat struct {
	node
	val float64
}

func (*ionFloat) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	switch flag {
	case flagNull: // null.float
		return &ionFloat{node: node{null: true}}, nil
	case 0:
		return &ionFloat{}, nil
	case 4:
		vr, err := copyVarData(flag, r)
		if err != nil {
			return nil, err
		}
		var fx float32
		if err = binary.Read(vr, binary.BigEndian, &fx); err != nil {
			return nil, err
		}
		return &ionFloat{val: float64(fx)}, nil
	case 8:
		vr, err := copyVarData(flag, r)
		if err != nil {
			return nil, err
		}
		var fx float64
		if err = binary.Read(vr, binary.BigEndian, &fx); err != nil {
			return nil, err
		}
		return &ionFloat{val: fx}, nil
	}
	return nil, errors.New("bad ion float data")
}

type ionDecimal struct {
	node
	val *apd.Decimal
}

func (*ionDecimal) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.decimal
		return &ionDecimal{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionDecimal{val: apd.New(0, 0)}, nil
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}
	coeff, neg, exponent, err := readDecimalParts(vr)
	if err != nil {
		return nil, err
	}
	if coeff.Sign() == 0 {
		// this is special case: +0 and -0 should be distinct
		ux := apd.New(0, exponent)
		if neg {
			ux.Negative = true
		}
		return &ionDecimal{val: ux}, nil
	}
	if neg {
		coeff.Neg(coeff)
	}
	return &ionDecimal{val: apd.NewWithBigInt(coeff, exponent)}, nil
}
