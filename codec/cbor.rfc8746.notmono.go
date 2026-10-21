// Copyright (c) 2012-2020 Ugorji Nwoke. All rights reserved.
// Use of this source code is governed by a MIT license found in the LICENSE file.

//go:build notmono || codec.notmono

package codec

import "reflect"

// RFC 8746 driver adapters for the generic (non-monomorphized) CBOR driver.

func (e *cborEncDriver[T]) cborTAEncUint(v uint64, bd byte) { e.encUint(v, bd) }

func (e *cborEncDriver[T]) cborTAWriteRaw(bs []byte) { e.w.writeb(bs) }

func (e *cborEncDriver[T]) cborTAEncodeNil() { e.EncodeNil() }

func (e *cborEncDriver[T]) cborTAEncodeTry(ti *typeInfo, rv reflect.Value) bool {
	if !e.h.TypedArrays {
		return false
	}
	return cborTAEncodeValue(e, e.h, ti, rv)
}

func (e *cborEncDriver[T]) cborTAEncodeTryIface(iv interface{}) bool {
	return cborTAEncodeTryIface(e, e.h, iv)
}

func (d *cborDecDriver[T]) cborTAPeekBd() byte {
	if !d.bdRead {
		d.readNextBd()
	}
	return d.bd
}

func (d *cborDecDriver[T]) cborTAU() uint64 { return d.decUint() }

func (d *cborDecDriver[T]) cborTAConsume() { d.bdRead = false }

func (d *cborDecDriver[T]) cborTAOctetsRead(n uint64) []byte {
	if n > uint64(cborTAMaxInt) {
		halt.errorf("cbor: RFC 8746 byte string length overflows platform int: %d", n)
	}
	bs := d.r.readx(uint(n))
	out := make([]byte, n)
	copy(out, bs)
	return out
}

func (d *cborDecDriver[T]) cborTADecodeInto(p interface{}, rt reflect.Type) {
	d.dec.decodeAs(p, rt, false)
}

func (d *cborDecDriver[T]) cborTADecodeTry(ti *typeInfo, rv0 reflect.Value) bool {
	return cborTAHandleTry(d, d.h, ti, rv0)
}

func (d *cborDecDriver[T]) cborTADecodeTryIface(iv interface{}) bool {
	return cborTADecodeTryIface(d, d.h, iv)
}

// cborTAHandleNaked decodes a recognized RFC 8746 tag while decoding an
// interface{}. It returns false for tags it does not handle.
func (d *cborDecDriver[T]) cborTAHandleNaked(tag uint64, n *fauxUnion) bool {
	return cborTAHandleNakedCommon(d, d.h, tag, n, d.DecodeNaked)
}
