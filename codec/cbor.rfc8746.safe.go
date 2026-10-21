// Copyright (c) 2012-2020 Ugorji Nwoke. All rights reserved.
// Use of this source code is governed by a MIT license found in the LICENSE file.

//go:build !go1.9 || safe || codec.safe || appengine

package codec

import "reflect"

// Safe builds never reinterpret memory; all element movement is
// order-explicit via reflect, so the native endianness is irrelevant.
const cborTAUnsafeFast = false

var cborTAHostLE = false

// cborTANativeBytes must never be called in safe builds (all call sites
// are gated by cborTAUnsafeFast).
func cborTANativeBytes(rv reflect.Value, esize uintptr) []byte {
	_ = rv
	_ = esize
	return nil
}
