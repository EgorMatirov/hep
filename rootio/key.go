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

const (
	kTitleMax = 32000
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
	left  int32  // number of bytes left in current block
	name  string // name of the object
	title string // title of the object

	buf []byte // buffer of the Key's value
	obj Object // Key's value
}

func newHeaderKey(name, title, class string, f *File, nbytes int32) Key {
	k := Key{
		datetime: time.Now(),
		name:     name,
		title:    title,
	}

	k.buildHeader(f, class, -1)

	k.keylen = int32(k.Sizeof())
	k.objlen = nbytes
	k.create(nbytes, f)
	return k
}

func (k *Key) buildHeader(dir *File, class string, filepos int64) {
	k.f = dir
	k.class = class
	if k.class == "TDirectoryFile" {
		// FIXME(sbinet): SetBit(kIsDirectoryFile)
		panic("rootio: not implemented")
	}

	k.version = 4 // FIXME(sbinet): TKey::Class_Version()

	if filepos == -1 && k.f != nil {
		filepos = k.f.end
	}

	if filepos > kStartBigFile {
		k.version += 1000
	}

	if len(k.title) > kTitleMax {
		k.title = string(k.title[:kTitleMax])
	}
}

func (k *Key) create(nbytes int32, f *File) {
	// FIXME(sbinet): SetUniqueID(keyAbsNumer);

	nsize := nbytes + k.keylen
	blks := &k.f.blocks

	// find free block
	best := blks.bestFree(nsize)
	if best == nil {
		panic(fmt.Errorf(
			"rootio: can not allocate %d bytes for ID=%q, Title=%q",
			nsize, k.Name(), k.Title(),
		))
	}

	k.seekkey = best.first

	// adding at the end of the file
	if k.seekkey > f.end {
		f.end = k.seekkey + int64(nsize)
		best.first = k.seekkey + int64(nsize)
		if f.end > best.last {
			best.last += 1000000000
		}
		k.left = -1
		k.buf = make([]byte, nsize)
	} else {
		k.left = int32(best.last-k.seekkey) - nsize + 1
	}

	// case where new object fills exactly a deleted gap
	k.bytes = nsize
	if k.left == 0 {
		k.buf = make([]byte, nsize)
		blks.remove(best)
	}

	// case where new object is placed in a deleted gap larger than itself
	if k.left > 0 {
		k.buf = make([]byte, nsize+4)
		left := -k.left // set header of remaining record
		wbuf := NewWBuffer(k.buf[nsize:], nil, 0)
		wbuf.WriteI32(left)
		best.first = k.seekkey + int64(nsize)
	}

	k.seekpdir = f.dir.seekdir
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

// Sizeof returns the size in bytes of the key header structure.
//
// An explanation about the nbytes variable used in this method.
// The size of seekkey and seekpdir is 8 instead of 4 if version is greater
// than 1000.
//
//   | Component         | Sizeof |
//   |-------------------|--------|
//   | fNbytes           | 4      |
//   | sizeof(Version_t) | 2      |
//   | fObjlen           | 4      |
//   | fKeylen           | 2      |
//   | fCycle            | 2      |
//   | fSeekKey          | 4 or 8 |
//   | fSeekPdir         | 4 or 8 |
//   | **TOTAL**         |   22   |
func (k *Key) Sizeof() int {
	nbytes := 22
	if k.version > 1000 {
		nbytes += 8
	}
	nbytes += datetimeSizeof(k.datetime)
	nbytes += tstringSizeof(k.class)
	nbytes += tstringSizeof(k.name)
	nbytes += tstringSizeof(k.title)
	return nbytes
}

func (k *Key) isCompressed() bool {
	return k.objlen != k.bytes-k.keylen
}

// UnmarshalROOT decodes the content of data into the Key
func (k *Key) UnmarshalROOT(r *RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	k.bytes = r.ReadI32()
	if k.bytes < 0 {
		k.class = "[GAP]"
		return nil
	}

	k.version = r.ReadI16()
	k.objlen = r.ReadI32()
	k.datetime = datime2time(r.ReadU32())
	k.keylen = int32(r.ReadI16())
	k.cycle = r.ReadI16()

	if k.version > 1000 {
		k.seekkey = r.ReadI64()
		k.seekpdir = r.ReadI64()
	} else {
		k.seekkey = int64(r.ReadI32())
		k.seekpdir = int64(r.ReadI32())
	}

	k.class = r.ReadString()
	k.name = r.ReadString()
	k.title = r.ReadString()

	myprintf("key-version: %v\n", k.version)
	myprintf("key-objlen:  %v\n", k.objlen)
	myprintf("key-cdate:   %v\n", k.datetime)
	myprintf("key-keylen:  %v\n", k.keylen)
	myprintf("key-cycle:   %v\n", k.cycle)
	myprintf("key-seekkey: %v\n", k.seekkey)
	myprintf("key-seekpdir:%v\n", k.seekpdir)
	myprintf("key-compress: %v %v %v %v %v\n", k.isCompressed(), k.objlen, k.bytes-k.keylen, k.bytes, k.keylen)
	myprintf("key-class: [%v]\n", k.class)
	myprintf("key-name:  [%v]\n", k.name)
	myprintf("key-title: [%v]\n", k.title)

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
