// Copyright 2017 The go-hep Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rootio

import "reflect"

type tobject struct {
	id   uint32 `rootio:"fUniqueID"`
	bits uint32 `rootio:"fBits"`
}

func (obj *tobject) Class() string {
	return "TObject"
}

func (obj *tobject) UnmarshalROOT(r *RBuffer) error {
	r.SkipVersion("")
	obj.id = r.ReadU32()
	obj.bits = r.ReadU32()
	obj.bits |= kIsOnHeap
	if obj.bits&kIsReferenced != 0 {
		_ = r.ReadU16()
	}
	return r.Err()
}

func (obj *tobject) MarshalROOT(w *WBuffer) (int, error) {
	panic("not implemented")
}

func init() {
	f := func() reflect.Value {
		o := &tobject{}
		return reflect.ValueOf(o)
	}
	Factory.add("TObject", f)
	Factory.add("*rootio.tobject", f)
}

var (
	_ Object          = (*tobject)(nil)
	_ ROOTMarshaler   = (*tobject)(nil)
	_ ROOTUnmarshaler = (*tobject)(nil)
)
