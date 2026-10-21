// Copyright (c) 2012-2020 Ugorji Nwoke. All rights reserved.
// Use of this source code is governed by a MIT license found in the LICENSE file.

//go:build !notmono && !codec.notmono

package codec

import "reflect"

// RFC 8746 driver adapters for the generated monomorphized CBOR drivers.

// ---- encoderCborBytes ----

func (e *cborEncDriverBytes) cborTAEncUint(v uint64, bd byte) { e.encUint(v, bd) }

func (e *cborEncDriverBytes) cborTAWriteRaw(bs []byte) { e.w.writeb(bs) }

func (e *cborEncDriverBytes) cborTAEncodeNil() { e.EncodeNil() }

func (e *cborEncDriverBytes) cborTAEncodeTry(ti *typeInfo, rv reflect.Value) bool {
	if !e.h.TypedArrays {
		return false
	}
	return cborTAEncodeValue(e, e.h, ti, rv)
}

func (e *cborEncDriverBytes) cborTAEncodeTryIface(iv interface{}) bool {
	return cborTAEncodeTryIface(e, e.h, iv)
}

// ---- encoderCborIO ----

func (e *cborEncDriverIO) cborTAEncUint(v uint64, bd byte) { e.encUint(v, bd) }

func (e *cborEncDriverIO) cborTAWriteRaw(bs []byte) { e.w.writeb(bs) }

func (e *cborEncDriverIO) cborTAEncodeNil() { e.EncodeNil() }

func (e *cborEncDriverIO) cborTAEncodeTry(ti *typeInfo, rv reflect.Value) bool {
	if !e.h.TypedArrays {
		return false
	}
	return cborTAEncodeValue(e, e.h, ti, rv)
}

func (e *cborEncDriverIO) cborTAEncodeTryIface(iv interface{}) bool {
	return cborTAEncodeTryIface(e, e.h, iv)
}

// ---- decoderCborBytes ----

func (d *cborDecDriverBytes) cborTAPeekBd() byte {
	if !d.bdRead {
		d.readNextBd()
	}
	return d.bd
}

func (d *cborDecDriverBytes) cborTAU() uint64 { return d.decUint() }

func (d *cborDecDriverBytes) cborTAConsume() { d.bdRead = false }

func (d *cborDecDriverBytes) cborTAOctetsRead(n uint64) []byte {
	if n > uint64(cborTAMaxInt) {
		halt.errorf("cbor: RFC 8746 byte string length overflows platform int: %d", n)
	}
	bs := d.r.readx(uint(n))
	out := make([]byte, n)
	copy(out, bs)
	return out
}

func (d *cborDecDriverBytes) cborTADecodeInto(p interface{}, rt reflect.Type) {
	d.dec.decodeAs(p, rt, false)
}

func (d *cborDecDriverBytes) cborTADecodeTry(ti *typeInfo, rv reflect.Value) bool {
	return cborTAHandleTry(d, d.h, ti, rv)
}

func (d *cborDecDriverBytes) cborTADecodeTryIface(iv interface{}) bool {
	return cborTADecodeTryIface(d, d.h, iv)
}

func (d *cborDecDriverBytes) cborTAHandleNaked(tag uint64, n *fauxUnion) bool {
	return cborTAHandleNakedCommon(d, d.h, tag, n, d.DecodeNaked)
}

// ---- decoderCborIO ----

func (d *cborDecDriverIO) cborTAPeekBd() byte {
	if !d.bdRead {
		d.readNextBd()
	}
	return d.bd
}

func (d *cborDecDriverIO) cborTAU() uint64 { return d.decUint() }

func (d *cborDecDriverIO) cborTAConsume() { d.bdRead = false }

func (d *cborDecDriverIO) cborTAOctetsRead(n uint64) []byte {
	if n > uint64(cborTAMaxInt) {
		halt.errorf("cbor: RFC 8746 byte string length overflows platform int: %d", n)
	}
	bs := d.r.readx(uint(n))
	out := make([]byte, n)
	copy(out, bs)
	return out
}

func (d *cborDecDriverIO) cborTADecodeInto(p interface{}, rt reflect.Type) {
	d.dec.decodeAs(p, rt, false)
}

func (d *cborDecDriverIO) cborTADecodeTry(ti *typeInfo, rv reflect.Value) bool {
	return cborTAHandleTry(d, d.h, ti, rv)
}

func (d *cborDecDriverIO) cborTADecodeTryIface(iv interface{}) bool {
	return cborTADecodeTryIface(d, d.h, iv)
}

func (d *cborDecDriverIO) cborTAHandleNaked(tag uint64, n *fauxUnion) bool {
	return cborTAHandleNakedCommon(d, d.h, tag, n, d.DecodeNaked)
}
