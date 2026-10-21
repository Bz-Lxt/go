// Copyright (c) 2012-2020 Ugorji Nwoke. All rights reserved.
// Use of this source code is governed by a MIT license found in the LICENSE file.

//go:build !safe && !codec.safe && !appengine && go1.21

package codec

import (
	"reflect"
	"unsafe"
)

const cborTAUnsafeFast = true

// cborTAHostLE records the native byte order of the host.
var cborTAHostLE bool

func init() {
	x := uint16(1)
	cborTAHostLE = *(*byte)(unsafe.Pointer(&x)) == 1
}

// cborTANativeBytes returns a native byte-order byte view over the
// elements of a numeric slice or fixed array.
func cborTANativeBytes(rv reflect.Value, esize uintptr) []byte {
	var p unsafe.Pointer
	if rv.Kind() == reflect.Slice {
		// a slice reflect.Value points at the slice header
		p = *(*unsafe.Pointer)((*unsafeReflectValue)(unsafe.Pointer(&rv)).ptr)
	} else {
		p = (*unsafeReflectValue)(unsafe.Pointer(&rv)).ptr
	}
	n := uintptr(rv.Len()) * esize
	return unsafe.Slice((*byte)(p), n)
}
