package kfx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
)

var ionBVM = []byte{0xE0, 1, 0, 0xEA} // binary version marker

func readValueStream(data []byte) ([]ionValue, error) {

	var values []ionValue

	// In the binary format, a value stream always starts with a binary version marker (BVM) that specifies the precise Ion version used to encode the data that follows
	if !bytes.HasPrefix(data, ionBVM) {
		return nil, errors.New("bad ion version marker in value stream")
	}

	for buf := bytes.NewReader(data[len(ionBVM):]); buf.Len() > 0; {
		// A value stream can contain other, perhaps different, BVMs interspersed with the top-level values.
		if marker, err := checkBVM(buf); err != nil {
			return nil, err
		} else if marker {
			continue
		}
		v, err := readValue(buf)
		if err != nil {
			return nil, err
		}
		if v != nil {
			values = append(values, v)
		}
	}
	return values, nil
}

func readValue(r *bytes.Reader) (ionValue, error) {
	b, err := r.ReadByte()
	if err != nil {
		return nil, errors.New("unsufficient data in value")
	}
	if b == ionBVM[0] {
		return nil, errors.New("unexpected ion version marker in value")
	}
	return valueCode(b>>4).read(uint64(b&0x0f), r)
}

func readAnnotatedValue(flag uint64, r *bytes.Reader) (ionValue, error) {

	if flag < 3 || flag > flagVarLen {
		return nil, fmt.Errorf("wrong annotation wrapper length (%d)", flag)
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}

	// annotations data
	ar, err := copyVarData(flagVarLen, vr)
	if err != nil {
		return nil, err
	}

	// wrapped value
	b, err := vr.ReadByte()
	if err != nil {
		return nil, errors.New("unsufficient data in value")
	}
	if valueCode(b>>4) == vcAnnotation {
		return nil, errors.New("annotation cannot wrap another annotation atomically")
	}
	err = vr.UnreadByte()
	if err != nil {
		return nil, err
	}
	v, err := readValue(vr)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, errors.New("annotation cannot annotate NOP padding")
	}

	// we have value, annotate it
	ta := make(typeAnnotations)
	for ar.Len() > 0 {
		a, err := readVarUInt64(ar)
		if err != nil {
			return nil, err
		}
		ta.add(a)
	}
	if len(ta) > 0 {
		v.setTA(ta)
	}
	return v, nil
}

func checkBVM(r *bytes.Reader) (bool, error) {
	for i, v := range ionBVM {
		b, err := r.ReadByte()
		if err != nil {
			return false, errors.New("bad ion version marker in value")
		}
		if v != b {
			if i == 0 {
				_ = r.UnreadByte()
				return false, nil
			}
			return false, errors.New("bad ion version marker in value")
		}
	}
	return true, nil
}

var errOverflow = errors.New("ion data overflows target type")

// readVarBigUInt reads arbitrary size VarUInt.
func readVarBigUInt(r *bytes.Reader) (*big.Int, error) {
	var ux *big.Int
	for r.Len() > 0 {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if ux == nil {
			// first byte doesn't need to shift and has one bit
			// to mask off - the stop bit
			ux = big.NewInt(int64(b & 0x7F))
		} else {
			ux.Lsh(ux, 7)
			ux.Or(ux, big.NewInt(int64(b&0x7F)))
		}
		if b&0x80 != 0 {
			return ux, nil
		}
	}
	// No stop-bit
	return nil, errOverflow
}

// readVarUInt64 reads VarUint into uint64.
// NOTE: optimization
func readVarUInt64(r *bytes.Reader) (uint64, error) {

	const maskHighBit = uint64(1) << 63

	// first byte doesn't need to shift and has one bit
	// to mask off - the stop bit
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	x := uint64(b & 0x7F)

	if b&0x80 != 0 {
		return x, nil
	}

	for x&maskHighBit == 0 {
		b, err := r.ReadByte()
		if err != nil {
			return x, err
		}
		x = x<<7 | uint64(b&0x7F)
		if b&0x80 != 0 {
			return x, nil
		}
	}
	return x, errOverflow
}

// readVarUInt64 reads VarUint into uint32.
// NOTE: optimization
func readVarUInt32(r *bytes.Reader) (uint32, error) {

	if val64, err := readVarUInt64(r); err != nil {
		return 0, err
	} else if val32 := uint32(val64); uint64(val32) == val64 {
		return val32, nil
	}
	return 0, errOverflow
}

// readVarBigInt reads arbitrary size VarInt.
func readVarBigInt(r *bytes.Reader) (*big.Int, error) {
	var ux *big.Int
	var neg bool
	for r.Len() > 0 {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if ux == nil {
			// first byte doesn't need to shift and has two bits
			// to mask off - the stop bit and the sign bit
			if b&0x40 != 0 {
				neg = true
			}
			ux = big.NewInt(int64(b & 0x3F))
		} else {
			ux.Lsh(ux, 7)
			ux.Or(ux, big.NewInt(int64(b&0x7F)))
		}
		if b&0x80 != 0 {
			if neg {
				ux.Neg(ux)
			}
			return ux, nil
		}
	}
	// No stop-bit
	return nil, errOverflow
}

// readVarInt64 reads Varint into int64.
// NOTE: optimization
func readVarInt64(r *bytes.Reader) (int64, error) {

	const maskHighBit = uint64(1) << 63

	// first byte doesn't need to shift and has two bits
	// to mask off - the stop bit and the sign bit
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	x := uint64(b & 0x3F)

	var neg bool
	if b&0x40 != 0 {
		neg = true
	}

	if b&0x80 != 0 {
		if neg {
			return -int64(x), nil
		}
		return int64(x), nil
	}

	for x&maskHighBit == 0 {
		b, err := r.ReadByte()
		if err != nil {
			return int64(x), err
		}
		x = x<<7 | uint64(b&0x7F)
		if b&0x80 != 0 {
			if neg {
				return -int64(x), nil
			}
			return int64(x), nil
		}
	}
	return int64(x), errOverflow
}

// readVarInt32 reads Varint into int32.
// NOTE: optimization
func readVarInt32(r *bytes.Reader) (int32, error) {

	if val64, err := readVarInt64(r); err != nil {
		return 0, err
	} else if val32 := int32(val64); int64(val32) == val64 {
		return val32, nil
	}
	return 0, errOverflow
}

// readBigUInt reads arbitrary size UInt.
func readBigUInt(r *bytes.Reader) (*big.Int, error) {

	ux := big.NewInt(0)
	if r.Len() == 0 {
		return ux, nil
	}

	buf := make([]byte, 0, r.Len())
	for r.Len() > 0 {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		buf = append(buf, b)
	}
	ux.SetBytes(buf)
	return ux, nil
}

// readUInt64 reads UInt into uint64.
// NOTE: optimization
func readUInt64(r *bytes.Reader) (uint64, error) {

	if r.Len() > 8 {
		return 0, errOverflow
	}

	var res uint64
	for r.Len() > 0 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		res = res<<8 | uint64(b)
	}
	return res, nil
}

// readUInt32 reads UInt into uint32.
// NOTE: optimization
func readUInt32(r *bytes.Reader) (uint32, error) {

	if val64, err := readUInt64(r); err != nil {
		return 0, err
	} else if val32 := uint32(val64); uint64(val32) == val64 {
		return val32, nil
	}
	return 0, errOverflow
}

// readBigIntAndSign reads arbitrary size Int as unsigned value and sign.
func readBigIntAndSign(r *bytes.Reader) (*big.Int, bool, error) {

	if r.Len() == 0 {
		return big.NewInt(0), false, nil
	}

	neg := false
	buf := make([]byte, 0, r.Len())
	for r.Len() > 0 {
		b, err := r.ReadByte()
		if err != nil {
			return nil, false, err
		}
		if len(buf) == 0 {
			if neg = b&0x80 != 0; neg {
				b &= 0x7f
			}
		}
		buf = append(buf, b)
	}
	ux := new(big.Int)
	ux.SetBytes(buf)
	return ux, neg, nil
}

// readBigInt reads arbitrary size Int.
func readBigInt(r *bytes.Reader) (*big.Int, error) {

	if r.Len() == 0 {
		return big.NewInt(0), nil
	}

	ux, neg, err := readBigIntAndSign(r)
	if err != nil {
		return nil, err
	}
	if neg {
		ux.Neg(ux)
	}
	return ux, nil
}

// readUInt64AndSign reads Int as unsigned uint64 and sign.
// NOTE: optimization
func readUInt64AndSign(r *bytes.Reader) (uint64, bool, error) {

	var ux uint64
	var neg bool

	if r.Len() == 0 {
		return ux, neg, nil
	}

	b, err := r.ReadByte()
	if err != nil {
		return ux, neg, err
	}

	if neg = b&0x80 != 0; neg {
		b &= 0x7f
	}

	if l := r.Len(); l > 0 {
		ux, err = readUInt64(r)
		if err != nil {
			return ux, neg, err
		}
		ux = uint64(b)<<(l*8) | ux
	} else {
		ux = uint64(b)
	}
	return ux, neg, nil
}

// castToInt64 casts uint64 to int64 with overflow validation.
func castToInt64(ux uint64, neg bool) (int64, error) {
	if !neg {
		if ux <= uint64(math.MaxInt64) {
			return int64(ux), nil
		}
		return 0, errOverflow
	}
	// abs(MinInt64) = MaxInt64 + 1
	if ux <= uint64(math.MaxInt64) {
		return -int64(ux), nil
	}
	if ux == uint64(math.MaxInt64+1) {
		return math.MinInt64, nil
	}
	return 0, errOverflow
}

// readInt64 - bool flag indicates negative zero.
func readInt64(r *bytes.Reader) (int64, bool, error) {

	ux, neg, err := readUInt64AndSign(r)
	if err != nil {
		return 0, false, err
	}
	x, err := castToInt64(ux, neg)
	if err != nil {
		return 0, false, err
	}
	if x == 0 && neg {
		return 0, true, nil
	}
	return 0, false, nil
}

// readInt32 - bool flag indicates negative zero.
func readInt32(r *bytes.Reader) (int32, bool, error) {

	if val64, flag, err := readInt64(r); err != nil {
		return 0, false, err
	} else if val32 := int32(val64); int64(val32) == val64 {
		return val32, flag, nil
	}
	return 0, false, errOverflow
}

// readDecimalPaarts reads arbitrary precision, base-10 encoded real number's components.
func readDecimalParts(r *bytes.Reader) (coeff *big.Int, neg bool, exponent int32, err error) {
	// NOTE: To fully adhere to Ion spec we should use readVarBigInt(r) but I do not want to handle arbitrary exponent
	exponent, err = readVarInt32(r)
	if err != nil {
		return
	}
	coeff, neg, err = readBigIntAndSign(r)
	if err != nil {
		return
	}
	return coeff, neg, exponent, nil
}

const (
	flagSortedStruct = 1
	flagVarLen       = 14
	flagNull         = 15
)

// skipVarData interprets size/length and properly advances input reader.
func skipVarData(size uint64, r *bytes.Reader) error {
	length := int64(size)
	if size == flagVarLen {
		// does not make sense to have len bigger than all available memory
		if ul, err := readVarUInt64(r); err == nil {
			length = int64(ul)
		} else {
			return err
		}
	}
	_, _ = r.Seek(length, io.SeekCurrent)
	return nil
}

// copyVarDataBytes interprets size/length and reads proper amount of data from input reader, returning it as byte slice for further interpretation.
func copyVarDataBytes(size uint64, r *bytes.Reader) ([]byte, error) {
	length := int64(size)
	if size == flagVarLen {
		// does not make sense to have len bigger than all available memory
		if ul, err := readVarUInt64(r); err == nil {
			length = int64(ul)
		} else {
			return nil, err
		}
	}
	b := make([]byte, length)
	if n, err := r.Read(b); err != nil {
		return nil, err
	} else if int64(n) != length {
		return nil, fmt.Errorf("unsufficient data in the ion stream, requred %d, have %d", length, n)
	}
	return b, nil
}

// copyVarData interprets size/length and reads proper amount of data from input reader, returning it as new reader for further interpretation.
func copyVarData(size uint64, r *bytes.Reader) (*bytes.Reader, error) {
	b, err := copyVarDataBytes(size, r)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}
