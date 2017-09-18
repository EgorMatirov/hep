// Copyright 2017 The go-hep Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rootio

import (
	"compress/zlib"
	"fmt"
	"io"
	"reflect"
	"time"
)

// noKeyError is the error returned when a rootio.Key could not be found.
type noKeyError struct {
	key string
	obj Named
}

func (err noKeyError) Error() string {
	return fmt.Sprintf("rootio: %s: could not find key %q", err.obj.Name(), err.key)
}

// Key is a key (a label) in a ROOT file
//
//  The Key class includes functions to book space on a file,
//   to create I/O buffers, to fill these buffers
//   to compress/uncompress data buffers.
//
//  Before saving (making persistent) an object on a file, a key must
//  be created. The key structure contains all the information to
//  uniquely identify a persistent object on a file.
//  The Key class is used by ROOT:
//    - to write an object in the Current Directory
//    - to write a new ntuple buffer
type Key struct {
	f *File // underlying file

	bytes    int32     // number of bytes for the compressed object+key
	version  int16     // version of the Key struct
	objlen   int32     // length of uncompressed object
	datetime time.Time // Date/Time when the object was written
	keylen   int32     // number of bytes for the Key struct
	cycle    int16     // cycle number of the object

	// address of the object on file (points to Key.bytes)
	// this is a redundant information used to cross-check
	// the data base integrity
	seekkey  int64
	seekpdir int64 // pointer to the directory supporting this object

	class string // object class name
	name  string // name of the object
	title string // title of the object

	buf []byte // buffer of the Key's value
	obj Object // Key's value
}

func (k *Key) Class() string {
	return k.class
}

func (k *Key) Name() string {
	return k.name
}

func (k *Key) Title() string {
	return k.title
}

func (k *Key) Cycle() int {
	return int(k.cycle)
}

// Value returns the data corresponding to the Key's value
func (k *Key) Value() interface{} {
	v, err := k.Object()
	if err != nil {
		panic(err)
	}
	return v
}

// Object returns the (ROOT) object corresponding to the Key's value.
func (k *Key) Object() (Object, error) {
	if k.obj != nil {
		return k.obj, nil
	}

	buf, err := k.Bytes()
	if err != nil {
		return nil, err
	}

	fct := Factory.Get(k.Class())
	if fct == nil {
		return nil, fmt.Errorf("rootio: no registered factory for class %q (key=%q)", k.Class(), k.Name())
	}

	v := fct()
	obj, ok := v.Interface().(Object)
	if !ok {
		return nil, fmt.Errorf("rootio: class %q does not implement rootio.Object (key=%q)", k.Class(), k.Name())
	}

	vv, ok := obj.(ROOTUnmarshaler)
	if !ok {
		return nil, fmt.Errorf("rootio: class %q does not implement rootio.ROOTUnmarshaler (key=%q)", k.Class(), k.Name())
	}

	err = vv.UnmarshalROOT(NewRBuffer(buf, nil, uint32(k.keylen)))
	if err != nil {
		return nil, err
	}

	if vv, ok := obj.(SetFiler); ok {
		vv.SetFile(k.f)
	}
	if dir, ok := obj.(*tdirectory); ok {
		dir.file = k.f
		err = dir.readKeys()
		if err != nil {
			return nil, err
		}
	}

	k.obj = obj
	return obj, nil
}

// Bytes returns the buffer of bytes corresponding to the Key's value
func (k *Key) Bytes() ([]byte, error) {
	data, err := k.load(nil)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (k *Key) load(buf []byte) ([]byte, error) {
	if len(buf) < int(k.objlen) {
		buf = make([]byte, k.objlen)
	}
	if len(k.buf) > 0 {
		copy(buf, k.buf)
		return buf, nil
	}
	if k.isCompressed() {
		// Note: this contains ZL[src][dst] where src and dst are 3 bytes each.
		// Won't bother with this for the moment, since we can cross-check against
		// objlen.
		const rootHDRSIZE = 9

		start := k.seekkey + int64(k.keylen) + rootHDRSIZE
		r := io.NewSectionReader(k.f, start, int64(k.bytes)-int64(k.keylen))
		rc, err := zlib.NewReader(r)
		if err != nil {
			return nil, err
		}
		_, err = io.ReadFull(rc, buf)
		if err != nil {
			return nil, err
		}
		return buf, nil
	}
	start := k.seekkey + int64(k.keylen)
	r := io.NewSectionReader(k.f, start, int64(k.bytes))
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (k *Key) isCompressed() bool {
	return k.objlen != k.bytes-k.keylen
}

// UnmarshalROOT decodes the content of data into the Key
func (k *Key) UnmarshalROOT(r *RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	r.ReadI32(&k.bytes)
	if k.bytes < 0 {
		k.class = "[GAP]"
		return nil
	}

	r.ReadI16(&k.version)
	r.ReadI32(&k.objlen)
	var u32 uint32
	r.ReadU32(&u32)
	k.datetime = datime2time(u32)
	var i16 int16
	r.ReadI16(&i16)
	k.keylen = int32(i16)
	r.ReadI16(&k.cycle)

	if k.version > 1000 {
		r.ReadI64(&k.seekkey)
		r.ReadI64(&k.seekpdir)
	} else {
		var i32 int32
		r.ReadI32(&i32)
		k.seekkey = int64(i32)
		r.ReadI32(&i32)
		k.seekpdir = int64(i32)
	}

	r.ReadString(&k.class)
	r.ReadString(&k.name)
	r.ReadString(&k.title)

	//k.pdat = data

	return r.Err()
}

func init() {
	f := func() reflect.Value {
		o := &Key{}
		return reflect.ValueOf(o)
	}
	Factory.add("TKey", f)
	Factory.add("*rootio.Key", f)
}

var _ Object = (*Key)(nil)
var _ Named = (*Key)(nil)
var _ ROOTUnmarshaler = (*Key)(nil)
