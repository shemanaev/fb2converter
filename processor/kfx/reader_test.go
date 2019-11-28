package kfx

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v2"
	"github.com/davecgh/go-spew/spew"
	td "github.com/maxatome/go-testdeep"
)

type binTestEntry struct {
	bin []byte
	res ionValue
	err error
	fmt string
	cmp func(t *testing.T, v, r ionValue, args ...interface{})
}

var tests []binTestEntry

func init() {

	var ok bool

	bi1 := new(big.Int)
	bi1, ok = bi1.SetString("0x123456789012345678", 0)
	if !ok {
		panic("Unable to set 0x123456789012345678")
	}
	bi2 := new(big.Int)
	bi2, ok = bi2.SetString("-0x123456789012345678", 0)
	if !ok {
		panic("Unable to set -0x123456789012345678")
	}

	negz := apd.New(0, 0)
	negz.Negative = true
	negz1 := apd.New(0, -1)
	negz1.Negative = true
	negz2 := apd.New(0, 1)
	negz2.Negative = true

	s1 := ionStruct{
		val:    make(map[uint64]ionValue),
		sorted: true,
	}
	s1.val[4] = &ionInt{val: big.NewInt(0)}

	s2 := ionStruct{
		val: make(map[uint64]ionValue),
	}
	s2.val[4] = &ionString{val: "a"}

	tests = []binTestEntry{
		{[]byte("\x0F"), &ionNull{node{null: true}}, nil, "%d: e_null()", nil},

		{[]byte("\x10"), &ionBool{node: node{null: false}, val: false}, nil, "%d: e_bool(False)", nil},
		{[]byte("\x11"), &ionBool{node: node{null: false}, val: true}, nil, "%d: e_bool(True)", nil},
		{[]byte("\x1F"), &ionBool{node: node{null: true}}, nil, "%d: e_bool()", nil},

		{[]byte("\x2F"), &ionInt{node: node{null: true}}, nil, "%d: e_int()", nil},
		{[]byte("\x20"), &ionInt{val: big.NewInt(0)}, nil, "%d: e_int(0)", nil},
		{[]byte("\x21\xFE"), &ionInt{val: big.NewInt(0xFE)}, nil, "%d: e_int(0xFE)", nil},
		{[]byte("\x22\x00\x01"), &ionInt{val: big.NewInt(1)}, nil, "%d: e_int(1)", nil},
		{[]byte("\x24\x01\x2F\xEF\xCC"), &ionInt{val: big.NewInt(0x12FEFCC)}, nil, "%d: e_int(0x12FEFCC)", nil},
		{[]byte("\x29\x12\x34\x56\x78\x90\x12\x34\x56\x78"), &ionInt{val: bi1}, nil, "%d:  e_int(0x123456789012345678)", nil},
		{[]byte("\x2E\x81\x05"), &ionInt{val: big.NewInt(5)}, nil, "%d: e_int(5) over padded length", nil},

		{[]byte("\x3F"), &ionInt{node: node{null: true}}, nil, "%d: e_int() null.int has two equivalent representations", nil},
		{[]byte("\x31\x01"), &ionInt{val: big.NewInt(-1)}, nil, "%d: e_int(-1)", nil},
		{[]byte("\x32\xC1\xC2"), &ionInt{val: big.NewInt(-0xC1C2)}, nil, "%d: e_int(-0xC1C2)", nil},
		{[]byte("\x36\xC1\xC2\x00\x00\x10\xFF"), &ionInt{val: big.NewInt(-0xC1C2000010FF)}, nil, "%d: e_int(-0xC1C2000010FF)", nil},
		{[]byte("\x39\x12\x34\x56\x78\x90\x12\x34\x56\x78"), &ionInt{val: bi2}, nil, "%d: e_int(-0x123456789012345678)", nil},
		{[]byte("\x3E\x82\x00\xA0"), &ionInt{val: big.NewInt(-160)}, nil, "%d: e_int(-160) over padded length + overpadded integer", nil},

		{[]byte("\x4F"), &ionFloat{node: node{null: true}}, nil, "%d: e_float()", nil},
		{[]byte("\x40"), &ionFloat{}, nil, "%d: e_float(0.0)", nil},
		{[]byte("\x44\x3F\x80\x00\x00"), &ionFloat{val: 1.0}, nil, "%d: e_float(1.0)", nil},
		{[]byte("\x44\x7F\x80\x00\x00"), &ionFloat{val: math.Inf(1)}, nil, "%d: e_float(float('+Inf'))", nil},
		{[]byte("\x48\x42\x02\xA0\x5F\x20\x00\x00\x00"), &ionFloat{val: 1.e10}, nil, "%d: e_float(1e10)", nil},
		{[]byte("\x48\x7F\xF8\x00\x00\x00\x00\x00\x00"), &ionFloat{val: math.NaN()}, nil, "%d: e_float(float('NaN'))", func(t *testing.T, v, r ionValue, args ...interface{}) {
			td.CmpNaN(t, v.(*ionFloat).val, args...)
		}},

		{[]byte("\x5F"), &ionDecimal{node: node{null: true}}, nil, "%d: e_decimal()", nil},
		{[]byte("\x50"), &ionDecimal{val: apd.New(0, 0)}, nil, "%d: e_decimal(Decimal())", nil},
		{[]byte("\x52\x47\xE8"), &ionDecimal{val: apd.New(0, -1000)}, nil, "%d: e_decimal(Decimal('0e-1000'))", nil},
		{[]byte("\x54\x07\xE8\x00\x00"), &ionDecimal{val: apd.New(0, 1000)}, nil, "%d: e_decimal(Decimal('0e1000'))", nil},
		{[]byte("\x52\x81\x01"), &ionDecimal{val: apd.New(1, 1)}, nil, "%d: e_decimal(Decimal('1e1'))", nil},
		{[]byte("\x53\xD4\x04\xD2"), &ionDecimal{val: apd.New(1234, -20)}, nil, "%d: e_decimal(Decimal('1234e-20'))", nil},
		{[]byte("\x52\x80\x01"), &ionDecimal{val: apd.New(1, 0)}, nil, "%d: e_decimal(Decimal('1e0'))", nil},
		{[]byte("\x52\xC1\x01"), &ionDecimal{val: apd.New(1, -1)}, nil, "%d: e_decimal(Decimal('1e-1'))", nil},
		{[]byte("\x51\xC1"), &ionDecimal{val: apd.New(0, -1)}, nil, "%d: e_decimal(Decimal('0e-1'))", nil}, // Should it be 0.0?
		{[]byte("\x51\x81"), &ionDecimal{val: apd.New(0, 1)}, nil, "%d: e_decimal(Decimal('0e1'))", nil},
		{[]byte("\x52\x81\x81"), &ionDecimal{val: apd.New(-1, 1)}, nil, "%d: e_decimal(Decimal('-1e1'))", nil},
		{[]byte("\x52\x80\x81"), &ionDecimal{val: apd.New(-1, 0)}, nil, "%d: e_decimal(Decimal('-1e0'))", nil},
		{[]byte("\x52\xC1\x81"), &ionDecimal{val: apd.New(-1, -1)}, nil, "%d: e_decimal(Decimal('-1e-1'))", nil},
		{[]byte("\x52\x80\x80"), &ionDecimal{val: negz}, nil, "%d: e_decimal(Decimal('-0'))", nil},
		{[]byte("\x52\xC1\x80"), &ionDecimal{val: negz1}, nil, "%d: e_decimal(Decimal('-0e-1'))", nil},
		{[]byte("\x52\x81\x80"), &ionDecimal{val: negz2}, nil, "%d: e_decimal(Decimal('-0e1'))", nil},

		{[]byte("\x6F"), &ionTimestamp{node: node{null: true}}, nil, "%d: e_timestamp()", nil},
		{[]byte("\x63\xC0\x0F\xE0"), &ionTimestamp{pres: tsYear, val: time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, "%d: e_timestamp(_ts(2016, precision=_PREC_YEAR)) -00:00", nil},
		{
			[]byte("\x63\x80\x0F\xE0"),
			&ionTimestamp{pres: tsYear, val: time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC), hasTZ: true, offTZ: 0}, nil,
			"%d: e_timestamp(_ts(2016, off_hours=0, precision=_PREC_YEAR))",
			nil,
		},
		{
			[]byte("\x64\x81\x0F\xE0\x82"),
			&ionTimestamp{pres: tsMonth, val: time.Date(2016, 2, 1, 0, 1, 0, 0, time.UTC), hasTZ: true, offTZ: time.Minute}, nil,
			"%d: e_timestamp(_ts(2016, 2, 1, 0, 1, off_minutes=1, precision=_PREC_MONTH))",
			nil,
		},
		{
			[]byte("\x65\xFC\x0F\xE0\x82\x82"),
			&ionTimestamp{pres: tsDay, val: time.Date(2016, 2, 1, 23, 0, 0, 0, time.UTC), hasTZ: true, offTZ: -time.Hour}, nil,
			"%d: e_timestamp(_ts(2016, 2, 1, 23, 0, off_hours=-1, precision=_PREC_DAY))",
			nil,
		},
		{
			[]byte("\x68\x43\xA4\x0F\xE0\x82\x82\x87\x80"),
			&ionTimestamp{pres: tsMin, val: time.Date(2016, 2, 2, 0, 0, 0, 0, time.UTC), hasTZ: true, offTZ: -time.Hour * 7}, nil,
			"%d: e_timestamp(_ts(2016, 2, 2, 0, 0, off_hours=-7, precision=_PREC_MINUTE))",
			nil,
		},
		{
			[]byte("\x69\x43\xA4\x0F\xE0\x82\x82\x87\x80\x9E"),
			&ionTimestamp{pres: tsSec, val: time.Date(2016, 2, 2, 0, 0, 30, 0, time.UTC), hasTZ: true, offTZ: -time.Hour * 7}, nil,
			"%d: e_timestamp(_ts(2016, 2, 2, 0, 0, off_hours=-7, precision=_PREC_MINUTE))",
			nil,
		},
		{
			[]byte("\x69\xC0\x81\x81\x81\x80\x80\x80\xC7\x01"),
			&ionTimestamp{pres: tsFrac, val: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
			fmt.Errorf("bad timestamp - fraction exponent out of bounds (%d) coefficient (%s)", -7, "1"),
			"%d: e_timestamp(_ts(1, 1, 1, 0, 0, 0, None, precision=_PREC_SECOND, fractional_precision=None, fractional_seconds=Decimal('1e-7')))",
			nil,
		},
		{
			[]byte("\x6B\x43\xA4\x0F\xE0\x82\x82\x87\x80\x9E\xC3\x01"),
			&ionTimestamp{pres: tsFrac, val: time.Date(2016, 2, 2, 0, 0, 30, 1000000, time.UTC), hasTZ: true, offTZ: -time.Hour * 7}, nil,
			"%d: e_timestamp(_ts(2016, 2, 2, 0, 0, 30, 1000, off_hours=-7, precision=_PREC_SECOND, fractional_precision=3))",
			nil,
		},
		{
			[]byte("\x67\xC0\x81\x81\x81\x80\x80\x80"),
			&ionTimestamp{pres: tsSec, val: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)}, nil,
			"%d: e_timestamp(_ts(year=1, month=1, day=1, precision=_PREC_SECOND))",
			nil,
		},
		{
			[]byte("\x67\xC1\x81\x81\x81\x80\x81\x80"),
			&ionTimestamp{pres: tsSec, val: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), hasTZ: true, offTZ: -time.Minute}, nil,
			"%d: e_timestamp(_ts(year=1, month=1, day=1, off_minutes=-1, precision=_PREC_SECOND))",
			nil,
		},
		{
			[]byte("\x69\xC1\x81\x81\x81\x80\x81\x80\x80\x00"),
			&ionTimestamp{pres: tsSec, val: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), hasTZ: true, offTZ: -time.Minute}, nil,
			"%d: e_timestamp(_ts(year=1, month=1, day=1, off_minutes=-1, precision=_PREC_SECOND)) Fractions with coefficients of 0 and exponents > -1 are ignored.",
			nil,
		},
		{
			[]byte("\x69\xC0\x81\x81\x81\x80\x80\x80\xC6\x01"),
			&ionTimestamp{pres: tsFrac, val: time.Date(1, 1, 1, 0, 0, 0, 1000, time.UTC)}, nil,
			"%d: e_timestamp(_ts(year=1, month=1, day=1, hour=0, minute=0, second=0, microsecond=1,p recision=_PREC_SECOND, fractional_precision=6))",
			nil,
		},
		{
			[]byte("\x6C\x43\xA4\x0F\xE0\x82\x82\x87\x80\x9E\xC6\x03\xE8"),
			&ionTimestamp{pres: tsFrac, val: time.Date(2016, 2, 2, 0, 0, 30, 1000000, time.UTC), hasTZ: true, offTZ: -time.Hour * 7}, nil,
			"%d: e_timestamp(_ts(2016, 2, 2, 0, 0, 30, 1000, off_hours=-7, precision=TimestampPrecision.SECOND)) The last three octets represent 1000d-6",
			nil,
		},

		{[]byte("\x7F"), &ionSymbol{node: node{null: true}}, nil, "%d: e_symbol()", nil},
		{[]byte("\x70"), &ionSymbol{}, nil, "%d: e_symbol(SYMBOL_ZERO_TOKEN)", nil},
		{[]byte("\x71\x02"), &ionSymbol{sid: 2}, nil, "%d: e_symbol(SymbolToken(None, 2))", nil},
		{[]byte("\x78\xFF\xFF\xFF\xFF\xFF\xFF\xFF\xFF"), &ionSymbol{sid: 0xFFFFFFFFFFFFFFFF}, nil, "%d: e_symbol(SymbolToken(None, 0xFFFFFFFFFFFFFFFF))", nil},

		{[]byte("\x8F"), &ionString{node: node{null: true}}, nil, "%d: e_string()", nil},
		{[]byte("\x80"), &ionString{}, nil, "%d: e_string(u'')", nil},
		{[]byte("\x84\xf0\x9f\x92\xa9"), &ionString{val: "\U0001F4A9"}, nil, "%d: e_string(u'\U0001F4A9')", nil},
		{[]byte("\x88$ion_1_0"), &ionString{val: "$ion_1_0"}, nil, "%d: e_string(u'$ion_1_0')", nil},

		{[]byte("\x9F"), &ionClob{node: node{null: true}}, nil, "%d: e_clob()", nil},
		{[]byte("\x90"), &ionClob{}, nil, "%d: e_clob(b'')", nil},
		{[]byte("\x94\xf0\x9f\x92\xa9"), &ionClob{val: []byte("\xf0\x9f\x92\xa9")}, nil, "%d: e_clob(b'\xf0\x9f\x92\xa9'))", nil},

		{[]byte("\xAF"), &ionBlob{node: node{null: true}}, nil, "%d: e_blob()", nil},
		{[]byte("\xA0"), &ionBlob{}, nil, "%d: e_blob(b'')", nil},
		{[]byte("\xA4\xf0\x9f\x92\xa9"), &ionBlob{val: []byte("\xf0\x9f\x92\xa9")}, nil, "%d: e_blob(b'\xf0\x9f\x92\xa9'))", nil},

		{[]byte("\xBF"), &ionList{node: node{null: true}}, nil, "%d: e_null_list()", nil},
		{[]byte("\xB0"), &ionList{}, nil, "%d: e_start_list(), e_end_list()", nil},

		{[]byte("\xCF"), &ionSexp{node: node{null: true}}, nil, "%d: e_null_sexp()", nil},
		{[]byte("\xC0"), &ionSexp{}, nil, "%d: e_start_sexp(), e_end_sexp()", nil},

		{[]byte("\xDF"), &ionStruct{node: node{null: true}}, nil, "%d: e_null_struct()", nil},
		{[]byte("\xD0"), &ionStruct{}, nil, "%d: e_start_struct(), e_end_struct()", nil},
		{[]byte("\xD1\x82\x84\x20"), &s1, nil, "%d: e_start_struct(), e_int(0, field_name=SymbolToken(None, 4)), e_end_struct()", nil},
		{[]byte("\xD7\x84\x81a\x80\x02\x01\x02"), &s2, nil, "%d: an example of struct with a single field with four total bytes of padding", nil},
		{[]byte("\xD2\x8F\x00"), &ionStruct{}, nil, "%d: empty struct, with a two byte pad", nil},
	}
}

func TestBinaryReader(t *testing.T) {

	for i, expected := range tests {
		t.Logf("-----------> Test "+expected.fmt, i)
		t.Logf("Test %d input: %s", i, spew.Sdump(expected.bin))
		got, err := readValue(bytes.NewReader(expected.bin))
		if err != nil {
			if expected.err == nil {
				t.Fatalf("test %d returned error - %s", i, err)
			}
			if err.Error() != expected.err.Error() {
				t.Fatalf("test %d returned error - %s", i, err)
			}
			continue
		}
		var buf bytes.Buffer
		spew.Fprintf(&buf, "Test %d expect %#v", i, expected.res)
		t.Log(buf.String())
		buf.Reset()
		spew.Fprintf(&buf, "Test %d result %#v", i, got)
		t.Log(buf.String())
		if expected.cmp != nil {
			expected.cmp(t, got, expected.res, expected.fmt, i)
		} else {
			td.Cmp(t, got, expected.res, expected.fmt, i)
		}
	}
}
