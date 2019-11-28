package kfx

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"
)

// ionNull represents IonNull node.
type ionNull struct {
	node
}

func (*ionNull) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.null
		return &ionNull{node: node{null: true}}, nil
	}
	// padding
	if err := skipVarData(flag, r); err != nil {
		return nil, err
	}
	return nil, nil
}

// ionBool represents IonBool node.
type ionBool struct {
	node
	val bool
}

func (*ionBool) fromBytes(flag uint64, _ *bytes.Reader) (ionValue, error) {
	switch flag {
	case flagNull: // null.bool
		return &ionBool{node: node{null: true}}, nil
	case 0: // false
		return &ionBool{node: node{null: false}, val: false}, nil
	case 1: // true
		return &ionBool{node: node{null: false}, val: true}, nil
	}
	return nil, fmt.Errorf("unknown ion bool value 0x%x", flag)
}

// Timestamp represents IonTimestamp node.

type tsPresision uint8

const (
	tsMaskYear tsPresision = 1 << iota
	tsMaskMonth
	tsMaskDay
	tsMaskMin
	tsMaskSec
	tsMaskFrac
)

const (
	tsYear  = tsPresision(0) | tsMaskYear // year
	tsMonth = tsYear | tsMaskMonth        // month
	tsDay   = tsMonth | tsMaskDay         // day
	tsMin   = tsDay | tsMaskMin           // minute
	tsSec   = tsMin | tsMaskSec           // second
	tsFrac  = tsSec | tsMaskFrac          // microsecond
)

func (ts tsPresision) String() string {
	switch ts {
	case tsYear:
		return "year"
	case tsMonth:
		return "month"
	case tsDay:
		return "day"
	case tsMin:
		return "minute"
	case tsSec:
		return "second"
	case tsFrac:
		return "millisecond"
	}
	return fmt.Sprintf("precision mask %x", uint8(ts))
}

type ionTimestamp struct {
	node
	val  time.Time
	pres tsPresision
	// TODO: we could  use *time.Location here instead of flag and offset, let's see what would be easier later
	hasTZ bool
	offTZ time.Duration
}

var daysPerMonth = [2][12]int{
	{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}, // normal
	{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}, // leap
}

func checkDay(year, month, day int) bool {
	if day < 1 || day > 31 {
		return false
	}
	if month < 1 || month > 12 {
		return false
	}
	if day > daysPerMonth[time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC).YearDay()-365][month-1] {
		return false
	}
	return true
}

func (*ionTimestamp) fromBytes(flag uint64, r *bytes.Reader) (io ionValue, err error) {

	if flag == flagNull {
		// null.timestamp
		return &ionTimestamp{node: node{null: true}}, nil
	}
	if flag == 0 {
		return nil, fmt.Errorf("unexpected flag for ion timestamp 0x%x", flag)
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}

	// required part
	var b byte
	var hasOffset bool
	var offset, year, month, day, hour, min, sec, nsec int

	// we need sign (negative zero is not a valid signed integer normally)
	b, err = vr.ReadByte()
	if err != nil {
		return nil, err
	}
	n := false
	if b&0x40 != 0 {
		n = true
	}
	offset = int(b & 0x3F)
	if b&0x80 == 0 {
		// we need second byte
		b, err = vr.ReadByte()
		if err != nil {
			return nil, err
		}
		offset = offset<<7 | int(b&0x7F)
		if b&0x80 == 0 {
			// Integer is too long to be an offset
			return nil, errOverflow
		}
	}
	hasOffset = true
	if n {
		if offset == 0 {
			hasOffset = false
		} else {
			offset = -offset
		}
	}

	// year is from 0001 to 9999 or 0x1 to 0x270F or 14 bits - 1 or 2 bytes
	b, err = vr.ReadByte()
	if err != nil {
		return nil, err
	}
	year = int(b & 0x7F)
	if b&0x80 == 0 {
		// we need second byte
		b, err = vr.ReadByte()
		if err != nil {
			return nil, err
		}
		year = year<<7 | int(b&0x7F)
		if b&0x80 == 0 {
			// Integer is too long to be a year
			return nil, errOverflow
		}
	}

	pres := tsYear
	month, day = 1, 1

	// we are ready now
	defer func() {
		if err == nil {
			// let's fill result - all parts are available now
			t := ionTimestamp{
				pres:  pres,
				val:   time.Date(year, time.Month(month), day, hour, min, sec, nsec, time.UTC),
				hasTZ: hasOffset,
				offTZ: time.Minute * time.Duration(offset),
			}
			if hasOffset {
				t.val = t.val.Add(t.offTZ)
			}
			io = &t
		}
	}()

	// Optional part

	b, err = vr.ReadByte()
	if err != nil {
		return nil, nil
	}
	month = int(b & 0x7F)
	pres = tsMonth

	b, err = vr.ReadByte()
	if err != nil {
		return nil, nil
	}
	day = int(b & 0x7F)
	pres = tsDay

	if !checkDay(year, month, day) {
		return nil, fmt.Errorf("bad timestamp - %d-%d-%d", year, month, day)
	}

	b, err = vr.ReadByte()
	if err != nil {
		return nil, nil
	}
	hour = int(b & 0x7F)

	b, err = vr.ReadByte()
	if err != nil {
		// hour and minutes should come together
		return nil, err
	}
	min = int(b & 0x7F)
	pres = tsMin

	b, err = vr.ReadByte()
	if err != nil {
		return nil, nil
	}
	sec = int(b & 0x7F)
	pres = tsSec

	// See if we have fractional part - "milliseconds since the epoch"
	if vr.Len() == 0 {
		return nil, nil
	}

	coeff, neg, exponent, err := readDecimalParts(vr)
	if err != nil {
		return nil, err
	}

	// According to the spec, fractions with coefficients of zero and exponents >= zero are ignored.
	if coeff.Sign() == 0 && exponent > -1 {
		return nil, nil
	}
	if neg {
		// Any other negative besides negative-zero is an error
		return nil, errors.New("bad timestamp - fraction cannot be negative")
	}

	if exponent < -6 || exponent > -1 {
		return nil, fmt.Errorf("bad timestamp - fraction exponent out of bounds (%d) coefficient (%s)", exponent, coeff.String())
	}

	coeff.Mul(coeff, new(big.Int).SetInt64(int64(time.Millisecond)))
	coeff.Quo(coeff, new(big.Int).SetInt64(int64(math.Pow10(-int(exponent)))))

	if !coeff.IsInt64() {
		return nil, fmt.Errorf("bad timestamp - fraction coefficient (%s)", coeff.String())
	}

	micros := coeff.Int64()
	if micros < 0 || micros > 999999 {
		return nil, fmt.Errorf("bad timestamp - fraction microseconds out of bounds (%d)", micros)
	}

	// full precision
	nsec = int(micros * int64(time.Microsecond))
	pres = tsFrac
	return nil, nil
}
