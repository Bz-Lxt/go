// Copyright (c) 2012-2020 Ugorji Nwoke. All rights reserved.
// Use of this source code is governed by a MIT license found in the LICENSE file.

package codec

// RFC 8746 "CBOR Tags for Well-Known Types" support:
//   - typed array tags 64..87 encode homogeneous numeric arrays as a tag
//     wrapping one definite-length byte string containing the concatenated
//     elements (sign, width and IEEE-754 bit patterns preserved).
//   - multi-dimensional rectangular arrays are encoded as tag 40
//     (row-major) or tag 1040 (column-major) wrapping [dims, content].
//   - tag 41 (homogeneous array) is accepted on decode.
//
// All wire-format logic lives here and is shared by the generic
// (notmono) driver and the generated mono (Bytes/IO) drivers, which only
// supply thin adapter methods.

import (
	"encoding/binary"
	"math"
	"reflect"
)

const (
	cborTATagRowMajor    uint64 = 40
	cborTATagHomogeneous uint64 = 41
	cborTATagColMajor    uint64 = 1040
)

const cborTAMaxInt = int(^uint(0) >> 1)

var cborTAHostIs64Bit = uint64(^uint(0)) == ^uint64(0)

// cborTATypeDesc describes one typed array tag (64..87).
type cborTATypeDesc struct {
	kind    reflect.Kind // Go element kind implied by the tag (float16 -> Float32)
	size    int          // bytes per element on the wire
	signed  bool
	isFloat bool
	le      bool
}

// cborTAParseTag maps an RFC 8746 typed array tag to its descriptor.
// ok is false for tag values outside 64..87, the reserved tag 76 and
// the 128-bit float tags 83/87.
func cborTAParseTag(tag uint64) (d cborTATypeDesc, ok bool) {
	if tag < 64 || tag > 87 {
		return
	}
	switch tag {
	case 68: // uint8-clamped: byte-identical payload to a uint8 typed array
		return cborTATypeDesc{kind: reflect.Uint8, size: 1}, true
	case 76: // reserved
		return
	case 80, 84: // float16 (big/little endian)
		return cborTATypeDesc{
			kind: reflect.Float32, size: 2, isFloat: true, le: tag == 84,
		}, true
	case 83, 87: // float128: no native Go type
		return
	}
	b := tag - 64
	d.isFloat = b&0x10 != 0
	d.signed = b&0x08 != 0
	d.le = b&0x04 != 0
	ll := int(b & 3)
	if d.isFloat {
		ll++
	}
	d.size = 1 << ll
	if d.isFloat {
		switch d.size {
		case 4:
			d.kind = reflect.Float32
		case 8:
			d.kind = reflect.Float64
		default:
			return
		}
	} else if d.signed {
		switch d.size {
		case 1:
			d.kind = reflect.Int8
		case 2:
			d.kind = reflect.Int16
		case 4:
			d.kind = reflect.Int32
		case 8:
			d.kind = reflect.Int64
		default:
			return
		}
	} else {
		switch d.size {
		case 1:
			d.kind = reflect.Uint8
		case 2:
			d.kind = reflect.Uint16
		case 4:
			d.kind = reflect.Uint32
		case 8:
			d.kind = reflect.Uint64
		default:
			return
		}
	}
	ok = true
	return
}

// cborTAEncodeTagForKind returns the little-endian typed array tag used on
// encode for a Go numeric kind, plus its wire element size.
func cborTAEncodeTagForKind(k reflect.Kind) (tag uint64, size int, ok bool) {
	switch k {
	case reflect.Uint8:
		return 64, 1, true
	case reflect.Int8:
		return 72, 1, true
	case reflect.Uint16:
		return 69, 2, true
	case reflect.Int16:
		return 77, 2, true
	case reflect.Uint32:
		return 70, 4, true
	case reflect.Int32:
		return 78, 4, true
	case reflect.Uint64:
		return 71, 8, true
	case reflect.Int64:
		return 79, 8, true
	case reflect.Float32:
		return 85, 4, true
	case reflect.Float64:
		return 86, 8, true
	case reflect.Int, reflect.Uint, reflect.Uintptr:
		if cborTAHostIs64Bit {
			if k == reflect.Int {
				return 79, 8, true
			}
			return 71, 8, true
		}
		if k == reflect.Int {
			return 78, 4, true
		}
		return 70, 4, true
	}
	return
}

func cborTAIsNumericLeaf(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func cborTAIntBits(k reflect.Kind) uint8 {
	switch k {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32:
		return 32
	case reflect.Int64, reflect.Uint64:
		return 64
	default: // Int, Uint, Uintptr
		if cborTAHostIs64Bit {
			return 64
		}
		return 32
	}
}

var cborTAKindTypes = map[reflect.Kind]reflect.Type{
	reflect.Int8:    reflect.TypeOf(int8(0)),
	reflect.Int16:   reflect.TypeOf(int16(0)),
	reflect.Int32:   reflect.TypeOf(int32(0)),
	reflect.Int64:   reflect.TypeOf(int64(0)),
	reflect.Uint8:   reflect.TypeOf(uint8(0)),
	reflect.Uint16:  reflect.TypeOf(uint16(0)),
	reflect.Uint32:  reflect.TypeOf(uint32(0)),
	reflect.Uint64:  reflect.TypeOf(uint64(0)),
	reflect.Float32: reflect.TypeOf(float32(0)),
	reflect.Float64: reflect.TypeOf(float64(0)),
}

// driver-level hooks used from the shared slice/array code paths.
type cborTAEncHook interface {
	cborTAEncodeTry(ti *typeInfo, rv reflect.Value) bool
	cborTAEncodeTryIface(iv interface{}) bool
}

type cborTADecHook interface {
	cborTADecodeTry(ti *typeInfo, rv reflect.Value) bool
	cborTADecodeTryIface(iv interface{}) bool
}

// cborTAIfaceIsArrayish reports whether iv is a slice/array value,
// optionally behind pointers. Raw (raw CBOR bytes) is excluded.
func cborTAIfaceIsArrayish(iv interface{}) bool {
	if _, ok := iv.(Raw); ok {
		return false
	}
	t := reflect.TypeOf(iv)
	if t == nil {
		return false
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Slice || t.Kind() == reflect.Array
}

// cborTAEncodeTryIface guards the top-level direct Encode entry points.
func cborTAEncodeTryIface(out cborTAEncOut, h *CborHandle, iv interface{}) bool {
	if !h.TypedArrays || !cborTAIfaceIsArrayish(iv) {
		return false
	}
	return cborTAEncodeValue(out, h, nil, reflect.ValueOf(iv))
}

// cborTADecodeTryIface guards the top-level direct Decode entry points.
func cborTADecodeTryIface(in cborTAStream, h *CborHandle, iv interface{}) bool {
	if !h.TypedArrays || !cborTAIfaceIsArrayish(iv) {
		return false
	}
	return cborTAHandleTry(in, h, nil, reflect.ValueOf(iv))
}

// ---------------- ENCODE ----------------

// cborTAEncOut is implemented by each concrete CBOR encoder driver via
// thin adapter methods.
type cborTAEncOut interface {
	cborTAEncUint(v uint64, bd byte)
	cborTAWriteRaw(bs []byte)
	cborTAEncodeNil()
}

// cborTAEncodeValue tries to encode rv as an RFC 8746 typed array.
// It returns false (having written nothing) when the value cannot be
// represented, so the caller falls back to the regular array encoding.
func cborTAEncodeValue(out cborTAEncOut, h *CborHandle, ti *typeInfo, rv0 reflect.Value) (handled bool) {
	if ti != nil && ti.mbs {
		return false
	}
	rv := rv0
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false
	}
	rt := rv.Type()

	leafKind, dims, shapeOK := cborTAShape(rt, rv)
	tag, esize, leafOK := cborTAEncodeTagForKind(leafKind)
	if !leafOK {
		return false
	}
	rank := len(dims)

	// nil outer slice: honor NilCollectionToZeroLength like other
	// collections; a zero-length typed array is a tag with an empty
	// byte string.
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		if !h.NilCollectionToZeroLength {
			out.cborTAEncodeNil()
			return true
		}
		if rank >= 2 {
			// no dims available for an empty multi-dimensional array:
			// fall back to a regular empty array
			return false
		}
		out.cborTAEncUint(tag, cborBaseTag)
		cborTAWriteBstr(out, nil)
		return true
	}

	if rank >= 2 {
		if !shapeOK {
			return false
		}
		for _, d := range dims {
			if d == 0 {
				// a zero dimension cannot be represented (dims must be
				// non-zero): keep the exact shape via classical encoding
				return false
			}
		}
		if !cborTACheckRect(rt, rv, dims, 0) {
			return false
		}
		out.cborTAEncUint(cborTATagRowMajor, cborBaseTag)
		out.cborTAEncUint(2, cborBaseArray)
		out.cborTAEncUint(uint64(rank), cborBaseArray)
		for _, d := range dims {
			out.cborTAEncUint(uint64(d), cborBaseUint)
		}
		out.cborTAEncUint(tag, cborBaseTag)
		buf := cborTAGather(rv, leafKind, esize, rank, dims)
		cborTAWriteBstr(out, buf)
		return true
	}

	if !cborTACheckRect(rt, rv, dims, 0) {
		return false
	}
	out.cborTAEncUint(tag, cborBaseTag)
	buf := cborTAGather(rv, leafKind, esize, rank, dims)
	cborTAWriteBstr(out, buf)
	return true
}

// cborTAWriteBstr emits a definite-length byte string.
func cborTAWriteBstr(out cborTAEncOut, bs []byte) {
	out.cborTAEncUint(uint64(len(bs)), cborBaseBytes)
	out.cborTAWriteRaw(bs)
}

// cborTAShape determines the leaf kind and the per-level lengths along
// the first row. ok is false when a zero-length level was encountered
// before the leaf (so only the leaf kind and known dims are reported).
func cborTAShape(rt reflect.Type, rv0 reflect.Value) (leaf reflect.Kind, dims []int, ok bool) {
	rv := rv0
	t := rt
	for {
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			n := rv.Len()
			dims = append(dims, n)
			if n == 0 {
				t = t.Elem()
				// report the statically-known remaining levels as zero so
				// the rank is preserved for empty values
				for t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
					dims = append(dims, 0)
					t = t.Elem()
				}
				return t.Kind(), dims, false
			}
			rv = rv.Index(0)
			t = t.Elem()
			continue
		}
		if cborTAIsNumericLeaf(t.Kind()) {
			return t.Kind(), dims, true
		}
		return t.Kind(), dims, false
	}
}

// cborTACheckRect verifies every row has exactly dims[level] elements
// and that the leaf is a supported numeric kind.
func cborTACheckRect(t reflect.Type, rv reflect.Value, dims []int, level int) bool {
	if level == len(dims) {
		return cborTAIsNumericLeaf(t.Kind())
	}
	if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
		return false
	}
	if rv.Len() != dims[level] {
		return false
	}
	for i := 0; i < rv.Len(); i++ {
		if !cborTACheckRect(t.Elem(), rv.Index(i), dims, level+1) {
			return false
		}
	}
	return true
}

// cborTAGather serializes the rectangular value row-major into a
// little-endian byte string payload.
func cborTAGather(rv0 reflect.Value, leafKind reflect.Kind, esize, rank int, dims []int) []byte {
	total := 1
	for i := 0; i < rank; i++ {
		total *= dims[i]
	}
	buf := make([]byte, total*esize)
	if cborTAUnsafeFast && cborTAHostLE {
		off := 0
		var walk func(rv reflect.Value, level int)
		walk = func(rv reflect.Value, level int) {
			if level == rank-1 {
				src := cborTANativeBytes(rv, uintptr(esize))
				copy(buf[off:off+len(src)], src)
				off += len(src)
				return
			}
			for i := 0; i < rv.Len(); i++ {
				walk(rv.Index(i), level+1)
			}
		}
		walk(rv0, 0)
		return buf
	}
	off := 0
	var walk func(rv reflect.Value, level int)
	walk = func(rv reflect.Value, level int) {
		if level == rank-1 {
			for i := 0; i < rv.Len(); i++ {
				ev := rv.Index(i)
				var u uint64
				switch leafKind {
				case reflect.Float32:
					u = uint64(math.Float32bits(float32(ev.Float())))
				case reflect.Float64:
					u = math.Float64bits(ev.Float())
				default:
					if leafKind == reflect.Uint || leafKind == reflect.Uint8 ||
						leafKind == reflect.Uint16 || leafKind == reflect.Uint32 ||
						leafKind == reflect.Uint64 || leafKind == reflect.Uintptr {
						u = ev.Uint()
					} else {
						u = uint64(ev.Int())
					}
				}
				cborTAPutLE(buf[off:], u, esize)
				off += esize
			}
			return
		}
		for i := 0; i < rv.Len(); i++ {
			walk(rv.Index(i), level+1)
		}
	}
	walk(rv0, 0)
	return buf
}

func cborTAPutLE(b []byte, u uint64, size int) {
	switch size {
	case 1:
		b[0] = byte(u)
	case 2:
		binary.LittleEndian.PutUint16(b, uint16(u))
	case 4:
		binary.LittleEndian.PutUint32(b, uint32(u))
	default:
		binary.LittleEndian.PutUint64(b, u)
	}
}

// ---------------- DECODE ----------------

// cborTAStream abstracts the driver primitives needed by the decoder.
type cborTAStream interface {
	cborTAPeekBd() byte
	cborTAU() uint64 // read the unsigned argument of the current descriptor
	cborTAConsume()  // mark the descriptor consumed
	cborTAOctetsRead(n uint64) []byte
	cborTADecodeInto(p interface{}, rt reflect.Type)
}

// cborTAHandleTry is the common entry point from slice/array decoders.
// It peeks at the stream and returns false (without consuming anything)
// when TypedArrays is off or the next item is not tagged.
func cborTAHandleTry(in cborTAStream, h *CborHandle, ti *typeInfo, rv0 reflect.Value) (handled bool) {
	if !h.TypedArrays {
		return false
	}
	bd := in.cborTAPeekBd()
	if bd>>5 != cborMajorTag {
		return false
	}
	tag := in.cborTAU()
	in.cborTAConsume()

	rv := rv0
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	switch {
	case tag >= 64 && tag <= 87:
		cborTADecode1D(in, tag, rv)
	case tag == cborTATagRowMajor:
		cborTADecodeMD(in, false, rv)
	case tag == cborTATagColMajor:
		cborTADecodeMD(in, true, rv)
	default:
		// tag 41 (homogeneous) or any other tag: decode the content
		// directly into the target as if the tag were absent.
		if !rv.CanAddr() {
			halt.errorf("cbor: cannot decode tagged content into non-addressable %v", rv.Type())
		}
		in.cborTADecodeInto(rv2i(rv.Addr()), rv.Type())
	}
	return true
}

// cborTAHandleNakedCommon implements interface{} decode for recognized
// RFC 8746 tags; selfNaked re-drives a naked decode (for tag 41).
func cborTAHandleNakedCommon(in cborTAStream, h *CborHandle, tag uint64, n *fauxUnion, selfNaked func()) bool {
	if !h.TypedArrays {
		return false
	}
	if tag == cborTATagHomogeneous {
		selfNaked()
		return true
	}
	if tag == cborTATagRowMajor || tag == cborTATagColMajor || (tag >= 64 && tag <= 87) {
		n.v = valueTypeExt
		n.x = cborTADecodeNaked(in, tag)
		return true
	}
	return false
}

// cborTADecode1D decodes a typed array tag wrapping a byte string into
// a 1-dimensional slice/array target.
func cborTADecode1D(in cborTAStream, tag uint64, rv reflect.Value) {
	td, ok := cborTAParseTag(tag)
	if !ok {
		halt.errorf("cbor: unsupported RFC 8746 typed array tag: %d", tag)
	}
	data := cborTAReadBstr(in)
	if uint64(len(data))%uint64(td.size) != 0 {
		halt.errorf("cbor: malformed RFC 8746 typed array: byte length %d is not a multiple of element size %d (tag %d)",
			len(data), td.size, tag)
	}
	count := len(data) / td.size
	cborTAEnsureLevel(rv, count)
	if count == 0 {
		return
	}
	cborTAFillFlat(in, td, data, nil, rv, nil, false)
}

// cborTADecodeMD decodes tag 40/1040 content: array(2) of [dims, content].
func cborTADecodeMD(in cborTAStream, colMajor bool, rv reflect.Value) {
	bd := in.cborTAPeekBd()
	if bd>>5 != cborMajorArray || bd == cborBdIndefiniteArray {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: expected definite array(2), got 0x%x", bd)
	}
	n := in.cborTAU()
	in.cborTAConsume()
	if n != 2 {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: outer array length %d, expected 2", n)
	}

	// dims
	bd = in.cborTAPeekBd()
	if bd>>5 != cborMajorArray || bd == cborBdIndefiniteArray {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: expected definite dims array, got 0x%x", bd)
	}
	rank := int(in.cborTAU())
	in.cborTAConsume()
	if rank < 1 {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: dims length must be >= 1")
	}
	dims := make([]int, rank)
	var product uint64 = 1
	for i := 0; i < rank; i++ {
		bd = in.cborTAPeekBd()
		if bd>>5 != cborMajorUint {
			halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: dim must be a non-zero unsigned int, got 0x%x", bd)
		}
		v := in.cborTAU()
		in.cborTAConsume()
		if v == 0 {
			halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: dimension %d is zero", i)
		}
		if v > uint64(cborTAMaxInt) {
			halt.errorf("cbor: RFC 8746 dimension %d overflows platform int: %d", i, v)
		}
		p2 := product * v
		if p2 < product {
			halt.errorf("cbor: RFC 8746 dimensions overflow element count")
		}
		product = p2
		dims[i] = int(v)
	}
	total := int(product)

	// content: typed array tag (64..87), homogeneous tag 41, or a
	// classical array.
	bd = in.cborTAPeekBd()
	var td cborTATypeDesc
	var data []byte
	var flat []interface{}
	switch {
	case bd>>5 == cborMajorTag:
		ctag := in.cborTAU()
		in.cborTAConsume()
		if ctag == cborTATagHomogeneous {
			flat = cborTAReadClassical(in, total)
			break
		}
		d, ok := cborTAParseTag(ctag)
		if !ok {
			halt.errorf("cbor: unsupported RFC 8746 content tag in multi-dimensional array: %d", ctag)
		}
		td = d
		data = cborTAReadBstr(in)
		want := product * uint64(td.size)
		if uint64(len(data)) != want {
			halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: content byte length %d, expected %d",
				len(data), want)
		}
	case bd>>5 == cborMajorArray:
		flat = cborTAReadClassical(in, total)
	default:
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: unexpected content descriptor 0x%x", bd)
	}

	cborTAValidateShape(rv.Type(), dims)
	cborTAEnsureNested(rv, rv.Type(), dims, 0)
	if total == 0 {
		return
	}
	cborTAFillFlat(in, td, data, flat, rv, dims, colMajor)
}

// cborTAReadBstr reads the definite-length byte string that must follow
// a typed array tag.
func cborTAReadBstr(in cborTAStream) (data []byte) {
	bd := in.cborTAPeekBd()
	if bd>>5 != cborMajorBytes || bd == cborBdIndefiniteBytes {
		halt.errorf("cbor: malformed RFC 8746 typed array: expected definite byte string, got 0x%x", bd)
	}
	n := in.cborTAU()
	in.cborTAConsume()
	if n > uint64(cborTAMaxInt) {
		halt.errorf("cbor: RFC 8746 byte string length overflows platform int: %d", n)
	}
	return in.cborTAOctetsRead(n)
}

// cborTAReadClassical reads a definite classical array of exactly total
// elements into a []interface{}.
func cborTAReadClassical(in cborTAStream, total int) []interface{} {
	bd := in.cborTAPeekBd()
	if bd>>5 != cborMajorArray || bd == cborBdIndefiniteArray {
		halt.errorf("cbor: malformed RFC 8746 array content: expected definite array, got 0x%x", bd)
	}
	n := int(in.cborTAU())
	if n != total {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: content length %d, expected %d", n, total)
	}
	var v []interface{}
	in.cborTADecodeInto(&v, intfSliceTyp)
	if len(v) != total {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: decoded %d elements, expected %d", len(v), total)
	}
	return v
}

// cborTAValidateShape checks the target type has rank nested
// slice/array levels ending in a numeric (or interface{}, for classical
// content) leaf, and that fixed array lengths match the dims.
func cborTAValidateShape(t reflect.Type, dims []int) {
	rank := len(dims)
	for level := 0; level < rank; level++ {
		switch t.Kind() {
		case reflect.Slice:
		case reflect.Array:
			if t.Len() != dims[level] {
				halt.errorf("cbor: cannot decode RFC 8746 array with dims %v into %v: dimension mismatch at %d",
					dims, t, level)
			}
		default:
			halt.errorf("cbor: cannot decode RFC 8746 array with dims %v into %v: expected slice/array at level %d",
				dims, t, level)
		}
		t = t.Elem()
	}
	if !cborTAIsNumericLeaf(t.Kind()) && t.Kind() != reflect.Interface {
		halt.errorf("cbor: cannot decode RFC 8746 numeric array into %v: unsupported element kind %v", t, t.Kind())
	}
}

// cborTAEnsureLevel grows/reuses a 1-dimensional slice target so it can
// hold n elements; verifies fixed arrays.
func cborTAEnsureLevel(rv reflect.Value, n int) {
	switch rv.Kind() {
	case reflect.Array:
		if rv.Len() != n {
			halt.errorf("cbor: cannot decode RFC 8746 array of %d elements into fixed array of %d", n, rv.Len())
		}
	case reflect.Slice:
		if rv.Len() == n {
			// mirror the classical decoder: an empty array produces a
			// non-nil zero-length slice
			if n == 0 && rv.IsNil() && rv.CanSet() {
				rv.Set(reflect.MakeSlice(rv.Type(), 0, 0))
			}
			return
		}
		if rv.Cap() >= n {
			rvSetSliceLen(rv, n)
			return
		}
		if !rv.CanSet() {
			halt.errorf("cbor: cannot decode RFC 8746 array of length %d into non-settable slice of capacity %d", n, rv.Cap())
		}
		rv.Set(reflect.MakeSlice(rv.Type(), n, n))
	default:
		halt.errorf("cbor: cannot decode RFC 8746 array into %v", rv.Type())
	}
}

// cborTAEnsureNested recursively sizes nested slice levels.
func cborTAEnsureNested(rv reflect.Value, t reflect.Type, dims []int, level int) {
	if level == len(dims) {
		return
	}
	if t.Kind() == reflect.Slice {
		n := dims[level]
		if rv.Len() != n {
			if rv.Cap() >= n {
				rvSetSliceLen(rv, n)
			} else {
				if !rv.CanSet() {
					halt.errorf("cbor: cannot resize non-settable slice %v to length %d", rv.Type(), n)
				}
				rv.Set(reflect.MakeSlice(t, n, n))
			}
		}
	}
	for i := 0; i < rv.Len(); i++ {
		cborTAEnsureNested(rv.Index(i), t.Elem(), dims, level+1)
	}
}

// cborTAFillFlat fills a 1D (dims == nil) or multi-dimensional target
// from a typed byte string (data) or a classical flat slice (flat).
func cborTAFillFlat(_ cborTAStream, td cborTATypeDesc, data []byte, flat []interface{},
	root reflect.Value, dims []int, colMajor bool) {
	rank := len(dims)
	var total int
	if data != nil {
		total = len(data) / td.size
	} else {
		total = len(flat)
	}
	t := root.Type()
	var lk reflect.Kind
	if rank == 0 {
		lk = t.Elem().Kind()
	} else {
		for i := 0; i < rank; i++ {
			t = t.Elem()
		}
		lk = t.Kind()
	}

	bulkExact := data != nil && !colMajor && cborTAUnsafeFast &&
		td.kind == lk && cborTAKindTypes[td.kind] != nil &&
		td.size == int(cborTAKindTypes[td.kind].Size()) && td.le == cborTAHostLE

	// bulk exact-width row-major fast path
	if bulkExact && rank <= 1 {
		dst := cborTANativeBytes(root, uintptr(td.size))
		copy(dst, data)
		return
	}
	if bulkExact && rank > 1 {
		off := 0
		var walk func(rv reflect.Value, level int)
		walk = func(rv reflect.Value, level int) {
			if level == rank-1 {
				n := dims[level] * td.size
				dst := cborTANativeBytes(rv, uintptr(td.size))
				copy(dst, data[off:off+n])
				off += n
				return
			}
			for i := 0; i < rv.Len(); i++ {
				walk(rv.Index(i), level+1)
			}
		}
		walk(root, 0)
		return
	}

	coords := make([]int, rank)
	for p := 0; p < total; p++ {
		if rank > 1 {
			if colMajor {
				// first dimension varies fastest
				q := p
				for i := 0; i < rank; i++ {
					coords[i] = q % dims[i]
					q /= dims[i]
				}
			} else {
				// last dimension varies fastest
				q := p
				for i := rank - 1; i >= 0; i-- {
					coords[i] = q % dims[i]
					q /= dims[i]
				}
			}
		}
		ev := root
		if rank == 0 {
			ev = root.Index(p)
		} else {
			for i := 0; i < rank; i++ {
				ev = ev.Index(coords[i])
			}
		}
		if flat != nil {
			cborTASetClassical(ev, flat[p])
			continue
		}
		u, i, f32b, f64b, isFloat := cborTAFetch(td, data, p)
		cborTASetTyped(ev, td, u, i, f32b, f64b, isFloat)
	}
}

// cborTAFetch extracts element p from the byte payload in its declared
// endianness and sign.
func cborTAFetch(td cborTATypeDesc, data []byte, p int) (u uint64, i int64, f32b uint32, f64b uint64, isFloat bool) {
	off := p * td.size
	var order binary.ByteOrder = binary.BigEndian
	if td.le {
		order = binary.LittleEndian
	}
	if td.isFloat {
		isFloat = true
		switch td.size {
		case 2:
			f32b = halfFloatToFloatBits(order.Uint16(data[off:]))
		case 4:
			f32b = order.Uint32(data[off:])
		case 8:
			f64b = order.Uint64(data[off:])
		}
		return
	}
	switch td.size {
	case 1:
		u = uint64(data[off])
	case 2:
		u = uint64(order.Uint16(data[off:]))
	case 4:
		u = uint64(order.Uint32(data[off:]))
	default:
		u = order.Uint64(data[off:])
	}
	if td.signed {
		if td.size < 8 {
			shift := uint(64 - uint(td.size)*8)
			i = int64(u<<shift) >> shift
		} else {
			i = int64(u)
		}
	}
	return
}

// cborTASetTyped assigns one typed-array element value to a destination
// reflect.Value, enforcing sign/width compatibility.
func cborTASetTyped(ev reflect.Value, td cborTATypeDesc,
	u uint64, i int64, f32b uint32, f64b uint64, isFloat bool) {
	tk := ev.Kind()
	if isFloat {
		switch tk {
		case reflect.Float32:
			switch td.size {
			case 2, 4:
				rvSetFloat32(ev, math.Float32frombits(f32b))
			default:
				halt.errorf("cbor: cannot decode RFC 8746 float64 typed array into %v without precision loss", ev.Type())
			}
		case reflect.Float64:
			switch td.size {
			case 2, 4:
				rvSetFloat64(ev, float64(math.Float32frombits(f32b)))
			case 8:
				rvSetFloat64(ev, math.Float64frombits(f64b))
			}
		default:
			halt.errorf("cbor: cannot decode RFC 8746 float typed array into %v", ev.Type())
		}
		return
	}
	switch tk {
	case reflect.Float32, reflect.Float64:
		// typed byte strings keep numeric families separate: an
		// integer typed array must not be widened into a float target
		halt.errorf("cbor: cannot decode RFC 8746 integer typed array (tag element %v) into float %v",
			td.kind, ev.Type())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := cborTAIntBits(tk)
		if td.signed {
			v := chkOvf.IntV(i, bits)
			cborTASetInt(ev, tk, v)
		} else {
			v := chkOvf.IntV(int64(u), bits)
			cborTASetInt(ev, tk, v)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		bits := cborTAIntBits(tk)
		if td.signed {
			if i < 0 {
				halt.errorf("cbor: cannot decode negative RFC 8746 value %d into unsigned %v", i, ev.Type())
			}
			u = uint64(i)
		}
		v := chkOvf.UintV(u, bits)
		cborTASetUint(ev, tk, v)
	default:
		halt.errorf("cbor: cannot decode RFC 8746 integer typed array into %v", ev.Type())
	}
}

func cborTASetInt(ev reflect.Value, k reflect.Kind, v int64) {
	switch k {
	case reflect.Int8:
		rvSetInt8(ev, int8(v))
	case reflect.Int16:
		rvSetInt16(ev, int16(v))
	case reflect.Int32:
		rvSetInt32(ev, int32(v))
	case reflect.Int64:
		rvSetInt64(ev, v)
	default:
		rvSetInt(ev, int(v))
	}
}

func cborTASetUint(ev reflect.Value, k reflect.Kind, v uint64) {
	switch k {
	case reflect.Uint8:
		rvSetUint8(ev, uint8(v))
	case reflect.Uint16:
		rvSetUint16(ev, uint16(v))
	case reflect.Uint32:
		rvSetUint32(ev, uint32(v))
	case reflect.Uint64:
		rvSetUint64(ev, v)
	case reflect.Uintptr:
		rvSetUintptr(ev, uintptr(v))
	default:
		rvSetUint(ev, uint(v))
	}
}

// cborTASetClassical assigns one element decoded from a classical CBOR
// array (naked number types) or, for interface{} leaves, stores it as-is.
func cborTASetClassical(ev reflect.Value, x interface{}) {
	if ev.Kind() == reflect.Interface {
		if x == nil {
			ev.Set(reflect.Zero(ev.Type()))
		} else {
			ev.Set(reflect.ValueOf(x))
		}
		return
	}
	switch v := x.(type) {
	case nil:
		// leave zero value
	case uint64:
		cborTASetTyped(ev, cborTATypeDesc{size: 8}, v, 0, 0, 0, false)
	case int64:
		cborTASetTyped(ev, cborTATypeDesc{size: 8, signed: true}, 0, v, 0, 0, false)
	case float64:
		switch ev.Kind() {
		case reflect.Float32:
			rvSetFloat32(ev, float32(v))
		case reflect.Float64:
			rvSetFloat64(ev, v)
		default:
			halt.errorf("cbor: cannot decode float value %v into %v", v, ev.Type())
		}
	default:
		halt.errorf("cbor: classical RFC 8746 array contains non-numeric element %v", x)
	}
}

// ---------------- interface{} (naked) decode ----------------

// cborTADecodeNaked decodes a typed/multi-dimensional array tag into a
// concrete Go value suitable for storage in an interface{}.
func cborTADecodeNaked(in cborTAStream, tag uint64) interface{} {
	if tag == cborTATagRowMajor || tag == cborTATagColMajor {
		return cborTADecodeMDNaked(in, tag == cborTATagColMajor)
	}
	td, ok := cborTAParseTag(tag)
	if !ok {
		halt.errorf("cbor: unsupported RFC 8746 typed array tag: %d", tag)
	}
	data := cborTAReadBstr(in)
	if uint64(len(data))%uint64(td.size) != 0 {
		halt.errorf("cbor: malformed RFC 8746 typed array: byte length %d is not a multiple of element size %d (tag %d)",
			len(data), td.size, tag)
	}
	count := len(data) / td.size
	rt := reflect.SliceOf(cborTAKindType(td))
	rv := reflect.MakeSlice(rt, count, count)
	if count > 0 {
		cborTAFillFlat(in, td, data, nil, rv, nil, false)
	}
	return rv.Interface()
}

func cborTAKindType(td cborTATypeDesc) reflect.Type {
	if t, ok := cborTAKindTypes[td.kind]; ok {
		return t
	}
	if cborTAIntBits(reflect.Int) == 64 {
		if td.signed {
			return reflect.TypeOf(int64(0))
		}
		return reflect.TypeOf(uint64(0))
	}
	if td.signed {
		return reflect.TypeOf(int32(0))
	}
	return reflect.TypeOf(uint32(0))
}

func cborTADecodeMDNaked(in cborTAStream, colMajor bool) interface{} {
	bd := in.cborTAPeekBd()
	if bd>>5 != cborMajorArray || bd == cborBdIndefiniteArray {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: expected definite array(2), got 0x%x", bd)
	}
	n := in.cborTAU()
	in.cborTAConsume()
	if n != 2 {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: outer array length %d, expected 2", n)
	}
	bd = in.cborTAPeekBd()
	if bd>>5 != cborMajorArray || bd == cborBdIndefiniteArray {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: expected definite dims array, got 0x%x", bd)
	}
	rank := int(in.cborTAU())
	in.cborTAConsume()
	if rank < 1 {
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: dims length must be >= 1")
	}
	dims := make([]int, rank)
	var product uint64 = 1
	for i := 0; i < rank; i++ {
		bd = in.cborTAPeekBd()
		if bd>>5 != cborMajorUint {
			halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: dim must be a non-zero unsigned int, got 0x%x", bd)
		}
		v := in.cborTAU()
		in.cborTAConsume()
		if v == 0 {
			halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: dimension %d is zero", i)
		}
		if v > uint64(cborTAMaxInt) {
			halt.errorf("cbor: RFC 8746 dimension %d overflows platform int: %d", i, v)
		}
		p2 := product * v
		if p2 < product {
			halt.errorf("cbor: RFC 8746 dimensions overflow element count")
		}
		product = p2
		dims[i] = int(v)
	}
	total := int(product)

	bd = in.cborTAPeekBd()
	var td cborTATypeDesc
	var data []byte
	var flat []interface{}
	classical := false
	switch {
	case bd>>5 == cborMajorTag:
		ctag := in.cborTAU()
		in.cborTAConsume()
		if ctag == cborTATagHomogeneous {
			flat = cborTAReadClassical(in, total)
			classical = true
			break
		}
		d, ok := cborTAParseTag(ctag)
		if !ok {
			halt.errorf("cbor: unsupported RFC 8746 content tag in multi-dimensional array: %d", ctag)
		}
		td = d
		data = cborTAReadBstr(in)
		want := product * uint64(td.size)
		if uint64(len(data)) != want {
			halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: content byte length %d, expected %d",
				len(data), want)
		}
	case bd>>5 == cborMajorArray:
		flat = cborTAReadClassical(in, total)
		classical = true
	default:
		halt.errorf("cbor: malformed RFC 8746 multi-dimensional array: unexpected content descriptor 0x%x", bd)
	}

	// build nested slices of the leaf type
	lt := reflect.TypeOf((*interface{})(nil)).Elem()
	if !classical {
		lt = cborTAKindType(td)
	}
	t := lt
	for i := rank - 1; i >= 0; i-- {
		t = reflect.SliceOf(t)
	}
	root := reflect.MakeSlice(t, dims[0], dims[0])
	cborTAEnsureNested(root, t, dims, 0)
	if total > 0 {
		cborTAFillFlat(in, td, data, flat, root, dims, colMajor)
	}
	return root.Interface()
}
