package kfx

import (
	"bytes"
)

// IonContainer
//     IonSequence
//         IonDatagram
//         IonList
//         IonSexp
//     IonStruct

type ionList struct {
	node
	val []ionValue
}

func (*ionList) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.list
		return &ionList{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionList{}, nil
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}
	var values []ionValue
	for vr.Len() > 0 {
		v, err := readValue(vr)
		if err != nil {
			return nil, err
		}
		if v != nil {
			values = append(values, v)
		}
	}
	if len(values) > 0 {
		return &ionList{val: values}, nil
	}
	return &ionList{}, nil
}

type ionSexp struct {
	node
	val []ionValue
}

func (*ionSexp) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.sexp
		return &ionSexp{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionSexp{}, nil
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}
	var values []ionValue
	for vr.Len() > 0 {
		v, err := readValue(vr)
		if err != nil {
			return nil, err
		}
		if v != nil {
			values = append(values, v)
		}
	}
	if len(values) > 0 {
		return &ionSexp{val: values}, nil
	}
	return &ionSexp{}, nil
}

type ionStruct struct {
	node
	val    map[uint64]ionValue
	sorted bool
}

func (*ionStruct) fromBytes(flag uint64, r *bytes.Reader) (ionValue, error) {
	if flag == flagNull {
		// null.struct
		return &ionStruct{node: node{null: true}}, nil
	}
	if flag == 0 {
		return &ionStruct{}, nil
	}
	var sorted bool
	if flag == flagSortedStruct {
		sorted = true
		flag = flagVarLen
	}
	vr, err := copyVarData(flag, r)
	if err != nil {
		return nil, err
	}
	values := make(map[uint64]ionValue)
	for vr.Len() > 0 {
		name, err := readVarUInt64(vr)
		if err != nil {
			return nil, err
		}
		v, err := readValue(vr)
		if err != nil {
			return nil, err
		}
		if v != nil {
			values[name] = v
		}
	}
	if len(values) > 0 {
		return &ionStruct{val: values, sorted: sorted}, nil
	}
	return &ionStruct{}, nil
}
