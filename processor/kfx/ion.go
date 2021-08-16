package kfx

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/amzn/ion-go/ion"
	"go.uber.org/zap"
)

var (
	ionBVM           = []byte{0xE0, 1, 0, 0xEA} // binary version marker
	rawFragmentTypes = []string{"$418", "$417"}
)

func createSST(name string, version int, maxID uint64) ion.SharedSymbolTable {
	symbols := make([]string, 0, maxID)
	m := len(ion.V1SystemSymbolTable.Symbols())
	for i := m + 1; i <= m+int(maxID); i++ {
		symbols = append(symbols, fmt.Sprintf("$%d", i))
	}
	return ion.NewSharedSymbolTable(name, version, symbols)
}

type dummyLST struct {
	ion.SymbolTable
}

// WriteTo does not do anything
func (*dummyLST) WriteTo(ion.Writer) error {
	return nil
}

func newDummyLST(lst ion.SymbolTable) *dummyLST {
	return &dummyLST{lst}
}

func createSymbolToken(stb ion.SymbolTableBuilder, sym string, log *zap.Logger) ion.SymbolToken {

	if !strings.HasPrefix(sym, "$") {
		sid, _ := stb.Add(sym)
		return ion.SymbolToken{Text: &sym, LocalSID: int64(sid)}
	}
	return createLocalSymbolToken(sym, log)
}

func createLocalSymbolToken(sym string, log *zap.Logger) ion.SymbolToken {

	if strings.HasPrefix(sym, "$") {
		// Strictly speaking this is only good while sid < YJ_symbols.MaxID
		if sid, err := strconv.ParseInt(sym[1:], 10, 64); err == nil {
			return ion.SymbolToken{Text: &sym, LocalSID: sid}
		}
	}
	// cannot parse symbol name - should never ever happen
	log.Warn("Unable to interpret symbol", zap.String("symbol", sym))
	return ion.SymbolToken{Text: &sym, LocalSID: ion.SymbolIDUnknown}
}

func dereferenceKfxIDs(r ion.Reader, stb ion.SymbolTableBuilder, eids map[int64]ion.SymbolToken, log *zap.Logger) ([]byte, error) {

	var process func(ion.Reader, ion.Writer) error

	stepIn := func(t ion.Type, r ion.Reader, w ion.Writer) error {
		err := r.StepIn()
		if err != nil {
			return err
		}
		switch t {
		case ion.ListType:
			err = w.BeginList()
		case ion.SexpType:
			err = w.BeginSexp()
		case ion.StructType:
			err = w.BeginStruct()
		}
		return err
	}

	stepOut := func(t ion.Type, r ion.Reader, w ion.Writer) error {
		err := r.StepOut()
		if err != nil {
			return err
		}
		switch t {
		case ion.ListType:
			err = w.EndList()
		case ion.SexpType:
			err = w.EndSexp()
		case ion.StructType:
			err = w.EndStruct()
		}
		return err
	}

	first := true

	next := func(r ion.Reader) bool {
		if first {
			first = false
			return true
		}
		return r.Next()
	}

	process = func(r ion.Reader, w ion.Writer) error {

		for next(r) {
			curT := r.Type()
			annots, err := r.Annotations()
			if err != nil {
				return err
			}
			name, err := r.FieldName()
			if name != nil && r.IsInStruct() {
				if err := w.FieldName(*name); err != nil {
					return err
				}
			}
			if r.IsNull() {
				if len(annots) > 0 {
					if err := w.Annotations(annots...); err != nil {
						return err
					}
				}
				if err := w.WriteNullType(curT); err != nil {
					return err
				}
				continue
			}
			println("AAAAAAAAAA", first, fmt.Sprintf("%+v", annots))
			if len(annots) == 1 && *annots[0].Text == "$598" {
				switch curT {
				case ion.StringType:
					if sym, err := r.StringValue(); err != nil {
						return fmt.Errorf("unable to read string value for derefenced KDF annotation: %w", err)
					} else if sym == nil {
						return errors.New("empty value for derefenced KDF annotation")
					} else {
						// To avoid additional resolution write an internal symbol form - we are building proper LST
						t := createSymbolToken(stb, *sym, log)
						text := fmt.Sprintf("$%d", t.LocalSID)
						if err := w.WriteSymbol(ion.SymbolToken{Text: &text, LocalSID: t.LocalSID}); err != nil {
							return fmt.Errorf("unable to write value <%s|%d> for derefenced KDF annotation: %w", *sym, t.LocalSID, err)
						}
						continue
					}
				case ion.IntType:
					// Only for dictionaries? In our cases eid translation table seems always empty.
					if eid, err := r.Int64Value(); err != nil {
						return fmt.Errorf("unable to read int64 value for derefenced KDF annotation: %w", err)
					} else if t, found := eids[*eid]; !found {
						log.Error("Undefined KDF annotation eid, ignoring", zap.Int64("eid", *eid), zap.Int("eids", len(eids)))
					} else {
						// To avoid additional resolution write an internal symbol form - we are building proper LST
						text := fmt.Sprintf("$%d", t.LocalSID)
						if err := w.WriteSymbol(ion.SymbolToken{Text: &text, LocalSID: t.LocalSID}); err != nil {
							return fmt.Errorf("unable to write value <%s|%d> for derefenced KDF annotation: %w", *t.Text, t.LocalSID, err)
						}
						continue
					}
				default:
					log.Error("Unexpected data type for dereferenced annotation in KDF fragment, ignoring", zap.Stringer("type", curT))
				}
			}
			switch curT {
			case ion.BoolType:
				v, err := r.BoolValue()
				if err != nil {
					return err
				}
				err = w.WriteBool(*v)
				if err != nil {
					return err
				}
			case ion.IntType:
				v, err := r.Int64Value()
				if err != nil {
					return err
				}
				err = w.WriteInt(*v)
				if err != nil {
					return err
				}
			case ion.FloatType:
				v, err := r.FloatValue()
				if err != nil {
					return err
				}
				err = w.WriteFloat(*v)
				if err != nil {
					return err
				}
			case ion.DecimalType:
				v, err := r.DecimalValue()
				if err != nil {
					return err
				}
				if v != nil {
					err = w.WriteDecimal(v)
					if err != nil {
						return err
					}
				}
			case ion.TimestampType:
				v, err := r.TimestampValue()
				if err != nil {
					return err
				}
				err = w.WriteTimestamp(*v)
				if err != nil {
					return err
				}
			case ion.SymbolType:
				v, err := r.SymbolValue()
				if err != nil {
					return err
				}
				if v != nil {
					err = w.WriteSymbol(*v)
					if err != nil {
						return err
					}
				}
			case ion.StringType:
				v, err := r.StringValue()
				if err != nil {
					return err
				}
				if v != nil {
					err = w.WriteString(*v)
					if err != nil {
						return err
					}
				}
			case ion.ClobType:
				v, err := r.ByteValue()
				if err != nil {
					return err
				}
				err = w.WriteClob(v)
				if err != nil {
					return err
				}
			case ion.BlobType:
				v, err := r.ByteValue()
				if err != nil {
					return err
				}
				err = w.WriteBlob(v)
				if err != nil {
					return err
				}
			case ion.ListType:
				fallthrough
			case ion.SexpType:
				fallthrough
			case ion.StructType:
				if err := stepIn(curT, r, w); err != nil {
					return fmt.Errorf("unable to step in container type for processing: %w", err)
				}
				if err := process(r, w); err != nil {
					return fmt.Errorf("unable to process container type: %w", err)
				}
				if err := stepOut(curT, r, w); err != nil {
					return fmt.Errorf("unable to step out container type for processing: %w", err)
				}
			default:
				panic("unknown type in derefValue: " + curT.String())
			}
		}
		return r.Err()
	}

	buf := new(bytes.Buffer)
	w := ion.NewBinaryWriterLST(buf, newDummyLST(stb.Build()))

	if err := process(r, w); err != nil {
		return nil, err
	}
	if err := w.Finish(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
