// Copyright 2017 The go-hep Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rootio

import "reflect"

type objarray struct {
	named named
	last  int
	arr   []Object
	low   int32
}

func (arr *objarray) Class() string {
	return "TObjArray"
}

func (arr *objarray) Name() string {
	return arr.named.Name()
}

func (arr *objarray) Title() string {
	return arr.named.Title()
}

func (arr *objarray) At(i int) Object {
	return arr.arr[i]
}

func (arr *objarray) Last() int {
	return arr.last
}

func (arr *objarray) Len() int {
	return len(arr.arr)
}

func (arr *objarray) LowerBound() int {
	return int(arr.low)
}

func (arr *objarray) UpperBound() int {
	panic("not implemented")
}

// ROOTUnmarshaler is the interface implemented by an object that can
// unmarshal itself from a ROOT buffer
func (arr *objarray) UnmarshalROOT(r *RBuffer) error {
	start := r.Pos()
	vers, pos, bcnt := r.ReadVersion()
	myprintf("objarray-vers=%v\n", vers)

	if vers > 2 {
		var obj tobject
		if err := obj.UnmarshalROOT(r); err != nil {
			return err
		}
	}
	if vers > 1 {
		arr.named.name = r.ReadString()
	}

	nobjs := int(r.ReadI32())
	arr.low = r.ReadI32()

	if vers != 3 {
		panic("boo")
	}

	arr.arr = make([]Object, nobjs)
	for i := range arr.arr {
		obj := r.ReadObjectAny()
		if obj != nil {
			arr.last = i
			arr.arr[i] = obj
		}
	}

	r.CheckByteCount(pos, bcnt, start, "TObjArray")
	return r.Err()
}

func init() {
	f := func() reflect.Value {
		o := &objarray{
			arr: make([]Object, 0),
		}
		return reflect.ValueOf(o)
	}
	Factory.add("TObjArray", f)
	Factory.add("*rootio.objarray", f)
}

var _ Object = (*objarray)(nil)
var _ Named = (*objarray)(nil)
var _ ObjArray = (*objarray)(nil)
var _ ROOTUnmarshaler = (*objarray)(nil)
