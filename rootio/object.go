// Copyright 2017 The go-hep Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rootio

import "reflect"

type tobject struct {
	id   uint32
	bits uint32
}

func (obj *tobject) Class() string {
	return "TObject"
}

func (obj *tobject) UnmarshalROOT(r *RBuffer) error {
	r.SkipVersion("")
	r.ReadU32(&obj.id)
	r.ReadU32(&obj.bits)
	obj.bits |= kIsOnHeap
	if obj.bits&kIsReferenced != 0 {
		var u16 uint16
		r.ReadU16(&u16)
	}
	return r.Err()
}

func init() {
	f := func() reflect.Value {
		o := &tobject{}
		return reflect.ValueOf(o)
	}
	Factory.add("TObject", f)
	Factory.add("*rootio.tobject", f)
}

var _ Object = (*tobject)(nil)
var _ ROOTUnmarshaler = (*tobject)(nil)
