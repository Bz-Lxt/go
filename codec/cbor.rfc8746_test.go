// Copyright (c) 2012-2020 Ugorji Nwoke. All rights reserved.
// Use of this source code is governed by a MIT license found in the LICENSE file.

package codec

import (
	"bytes"
	"encoding/hex"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func rfc8746Handle() *CborHandle {
	h := new(CborHandle)
	h.TypedArrays = true
	return h
}

func rfc8746EncBytes(t *testing.T, h *CborHandle, v interface{}) []byte {
	var bs []byte
	NewEncoderBytes(&bs, h).MustEncode(v)
	return bs
}

func rfc8746EncIO(t *testing.T, h *CborHandle, v interface{}) []byte {
	var buf bytes.Buffer
	NewEncoder(&buf, h).MustEncode(v)
	return buf.Bytes()
}

func rfc8746RoundTrip(t *testing.T, h *CborHandle, v, vp interface{}) {
	t.Helper()
	for _, enc := range []func() []byte{
		func() []byte { return rfc8746EncBytes(t, h, v) },
		func() []byte { return rfc8746EncIO(t, h, v) },
	} {
		bs := enc()
		NewDecoderBytes(bs, h).MustDecode(vp)
		// exercise the io.Reader driver adapters as well
		NewDecoder(bytes.NewReader(bs), h).MustDecode(vp)
	}
}

func rfc8746Hex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rfc8746ExpectErr(t *testing.T, bs []byte, h *CborHandle, v interface{}) {
	t.Helper()
	if err := NewDecoderBytes(bs, h).Decode(v); err == nil {
		t.Fatalf("expected error decoding %x into %T, got value %#v", bs, v, v)
	}
}

func TestCborRfc8746OneDSlices(t *testing.T) {
	h := rfc8746Handle()

	type tc struct {
		v interface{}
		p interface{}
	}
	cases := []tc{
		{&[]int8{-128, -1, 0, 1, 127}, new([]int8)},
		{&[]int16{-32768, -1, 0, 1, 32767}, new([]int16)},
		{&[]int32{math.MinInt32, -1, 0, 1, math.MaxInt32}, new([]int32)},
		{&[]int64{math.MinInt64, -1, 0, 1, math.MaxInt64}, new([]int64)},
		{&[]uint8{0, 1, 200, 255}, new([]uint8)},
		{&[]uint16{0, 1, 65535}, new([]uint16)},
		{&[]uint32{0, 1, math.MaxUint32}, new([]uint32)},
		{&[]uint64{0, 1, math.MaxUint64}, new([]uint64)},
	}
	// platform-width int/uint extremes without untyped constants that
	// overflow on 32-bit targets
	minInt := ^int(0) << (strconv.IntSize - 1)
	cases = append(cases,
		tc{&[]int{minInt, 0, ^minInt}, new([]int)},
		tc{&[]uint{0, ^uint(0)}, new([]uint)},
		tc{&[]uintptr{0, ^uintptr(0)}, new([]uintptr)},
	)
	cases = append(cases,
		// IEEE-754 bit patterns, incl NaN/Inf/-0/subnormal
		tc{&[]float32{0, 1, -1, float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Copysign(0, -1)),
			math.Float32frombits(0x00000001), math.Float32frombits(0x7fc00001), math.Float32frombits(0xffc0abcd)},
			new([]float32)},
		tc{&[]float64{0, 1, -1, math.Inf(1), math.Inf(-1), math.Copysign(0, -1),
			math.Float64frombits(0x0000000000000001), math.Float64frombits(0x7ff8000000000001),
			math.Float64frombits(0xfff000000000dead)},
			new([]float64)},
	)
	for _, c := range cases {
		rfc8746RoundTrip(t, h, c.v, c.p)
		got := reflect.ValueOf(c.p).Elem()
		want := reflect.ValueOf(c.v).Elem()
		if got.Len() != want.Len() {
			t.Fatalf("%T: length mismatch %d != %d", c.v, got.Len(), want.Len())
		}
		for i := 0; i < want.Len(); i++ {
			g, w := got.Index(i).Interface(), want.Index(i).Interface()
			if !rfc8746BitsEqual(g, w) {
				t.Fatalf("%T[%d]: %v(%x) != %v(%x)", c.v, i, g, rfc8746Bits(g), w, rfc8746Bits(w))
			}
		}
	}
}

func rfc8746BitsEqual(a, b interface{}) bool {
	switch a.(type) {
	case float32:
		return math.Float32bits(a.(float32)) == math.Float32bits(b.(float32))
	case float64:
		return math.Float64bits(a.(float64)) == math.Float64bits(b.(float64))
	default:
		return a == b
	}
}

func rfc8746Bits(v interface{}) uint64 {
	switch x := v.(type) {
	case float32:
		return uint64(math.Float32bits(x))
	case float64:
		return math.Float64bits(x)
	default:
		return 0
	}
}

func TestCborRfc8746FixedArrays(t *testing.T) {
	h := rfc8746Handle()
	var a [5]int16
	want := [5]int16{-1, 0, 1, 2, 32767}
	rfc8746RoundTrip(t, h, &want, &a)
	if a != want {
		t.Fatalf("got %v want %v", a, want)
	}

	// 1D fixed array length mismatch is an error
	bs := rfc8746EncBytes(t, h, &[3]uint32{1, 2, 3})
	var b [4]uint32
	rfc8746ExpectErr(t, bs, h, &b)
}

func TestCborRfc8746LargeSlice(t *testing.T) {
	h := rfc8746Handle()
	const n = 10000
	v := make([]float64, n)
	for i := range v {
		v[i] = float64(i) * 1.5
	}
	var v2 []float64
	rfc8746RoundTrip(t, h, &v, &v2)
	if len(v2) != n {
		t.Fatalf("len %d", len(v2))
	}
	for i := range v {
		if math.Float64bits(v[i]) != math.Float64bits(v2[i]) {
			t.Fatalf("at %d", i)
		}
	}
}

func TestCborRfc8746WireTags(t *testing.T) {
	h := rfc8746Handle()
	check := func(v interface{}, wantTag byte) {
		bs := rfc8746EncBytes(t, h, v)
		if bs[0] != 0xd8 || bs[1] != wantTag {
			t.Fatalf("%T: expected tag %d, got %x", v, wantTag, bs[:2])
		}
		// byte string follows
		if bs[2]>>5 != cborMajorBytes {
			t.Fatalf("%T: expected byte string, got 0x%x", v, bs[2])
		}
	}
	check(&[]uint8{1, 2}, 64)
	check(&[]int8{-1}, 72)
	check(&[]uint16{1}, 69)
	check(&[]int16{-1}, 77)
	check(&[]uint32{1}, 70)
	check(&[]int32{-1}, 78)
	check(&[]uint64{1}, 71)
	check(&[]int64{-1}, 79)
	check(&[]float32{1}, 85)
	check(&[]float64{1}, 86)
	if strconv.IntSize == 64 {
		check(&[]int{-1}, 79)
		check(&[]uint{1}, 71)
	} else {
		check(&[]int{-1}, 78)
		check(&[]uint{1}, 70)
	}
}

func TestCborRfc8746Endianness(t *testing.T) {
	h := rfc8746Handle()
	// []uint16{0x0102, 0x0304} big-endian tag 65 and little-endian tag 69
	be := rfc8746Hex(t, "d8414401020304")
	le := rfc8746Hex(t, "d8454402010403")
	var v1, v2 []uint16
	NewDecoderBytes(be, h).MustDecode(&v1)
	NewDecoderBytes(le, h).MustDecode(&v2)
	want := []uint16{0x0102, 0x0304}
	if !reflect.DeepEqual(v1, want) || !reflect.DeepEqual(v2, want) {
		t.Fatalf("be=%v le=%v want=%v", v1, v2, want)
	}
	// encode uses little-endian
	if !bytes.Equal(rfc8746EncBytes(t, h, &want), le) {
		t.Fatalf("encode not little-endian: %x", rfc8746EncBytes(t, h, &want))
	}

	// float64 big-endian tag 82: {1.0} = 3ff0000000000000
	var fv []float64
	NewDecoderBytes(rfc8746Hex(t, "d852483ff0000000000000"), h).MustDecode(&fv)
	if len(fv) != 1 || math.Float64bits(fv[0]) != 0x3ff0000000000000 {
		t.Fatalf("be float64: %x", fv)
	}

	// float16 tags decode into float32; tag 80 BE 0x3c00 = 1.0, tag 84 LE 0x003c
	var hf1, hf2 []float32
	NewDecoderBytes(rfc8746Hex(t, "d850423c00"), h).MustDecode(&hf1)
	NewDecoderBytes(rfc8746Hex(t, "d85442003c"), h).MustDecode(&hf2)
	if hf1[0] != 1 || hf2[0] != 1 {
		t.Fatalf("float16: %v %v", hf1, hf2)
	}
	// uint8-clamped tag 68 accepted
	var uv []uint8
	NewDecoderBytes(rfc8746Hex(t, "d844420102"), h).MustDecode(&uv)
	if len(uv) != 2 || uv[0] != 1 || uv[1] != 2 {
		t.Fatalf("clamped uint8: %v", uv)
	}
}

func TestCborRfc8746Empty(t *testing.T) {
	h := rfc8746Handle()

	// empty non-nil slice -> tag + empty bstr
	empty := []uint32{}
	bs := rfc8746EncBytes(t, h, &empty)
	if !bytes.Equal(bs, rfc8746Hex(t, "d84640")) {
		t.Fatalf("empty: %x", bs)
	}
	var got []uint32
	NewDecoderBytes(bs, h).MustDecode(&got)
	if got == nil || len(got) != 0 {
		t.Fatalf("empty decode: %#v", got)
	}

	// nil slice -> nil by default
	var nilSlice []int16
	bs = rfc8746EncBytes(t, h, &nilSlice)
	if !bytes.Equal(bs, []byte{0xf6}) {
		t.Fatalf("nil default: %x", bs)
	}
	var got2 []int16
	NewDecoderBytes(bs, h).MustDecode(&got2)
	if got2 != nil {
		t.Fatalf("nil decode: %#v", got2)
	}

	// NilCollectionToZeroLength -> empty typed array
	h2 := rfc8746Handle()
	h2.NilCollectionToZeroLength = true
	bs = rfc8746EncBytes(t, h2, &nilSlice)
	if !bytes.Equal(bs, rfc8746Hex(t, "d84d40")) {
		t.Fatalf("nil zero-length: %x", bs)
	}

	// zero-size multi-dimensional slice falls back to classical empty array
	mdEmpty := [][]int16{}
	bs = rfc8746EncBytes(t, h, &mdEmpty)
	if !bytes.Equal(bs, []byte{0x80}) {
		t.Fatalf("md empty: %x", bs)
	}
}

func TestCborRfc8746MultiDim(t *testing.T) {
	h := rfc8746Handle()

	// RFC 8746 Figure 1 golden: tag40, [[2,3], tag65, 12 bytes BE uint16]
	golden := rfc8746Hex(t, "d82882820203d8414c000200040008000400100100")
	var fixed [2][3]uint16
	NewDecoderBytes(golden, h).MustDecode(&fixed)
	wantFixed := [2][3]uint16{{2, 4, 8}, {4, 16, 256}}
	if fixed != wantFixed {
		t.Fatalf("fixed: %v want %v", fixed, wantFixed)
	}
	var nested [][]uint16
	NewDecoderBytes(golden, h).MustDecode(&nested)
	if !reflect.DeepEqual(nested, [][]uint16{{2, 4, 8}, {4, 16, 256}}) {
		t.Fatalf("nested: %v", nested)
	}
	var mixed [][3]uint16
	NewDecoderBytes(golden, h).MustDecode(&mixed)
	if !reflect.DeepEqual(mixed, [][3]uint16{{2, 4, 8}, {4, 16, 256}}) {
		t.Fatalf("mixed: %v", mixed)
	}

	// round-trip all shapes/kinds via both drivers
	encs := [][][]int32{
		{{1, 2, 3}, {4, 5, 6}},
		{{-1, -2}, {-3, -4}, {5, 6}},
	}
	for _, v := range encs {
		var v2 [][]int32
		rfc8746RoundTrip(t, h, &v, &v2)
		if !reflect.DeepEqual(v, v2) {
			t.Fatalf("[][]int32 %v != %v", v, v2)
		}
	}

	// fixed 3D float32 with bit patterns
	v3 := [2][3][2]float32{
		{{1, math.Float32frombits(0x7fc0abcd)}, {2, 3}, {4, 5}},
		{{6, 7}, {8, 9}, {float32(math.Inf(1)), -0}},
	}
	var v3b [2][3][2]float32
	rfc8746RoundTrip(t, h, &v3, &v3b)
	for i := range v3 {
		for j := range v3[i] {
			for k := range v3[i][j] {
				if math.Float32bits(v3[i][j][k]) != math.Float32bits(v3b[i][j][k]) {
					t.Fatalf("3d bits at %d %d %d", i, j, k)
				}
			}
		}
	}

	// non-rectangular slices fall back to classical arrays
	jagged := [][]int16{{1, 2}, {3}}
	var jb [][]int16
	rfc8746RoundTrip(t, h, &jagged, &jb)
	if !reflect.DeepEqual(jagged, jb) {
		t.Fatalf("jagged: %v", jb)
	}
}

func TestCborRfc8746ColumnMajor(t *testing.T) {
	h := rfc8746Handle()
	// shape [2][3], column-major order of the RFC figure:
	// (0,0)=2,(1,0)=4,(0,1)=8,(1,1)=4,(0,2)=16,(1,2)=256
	golden := rfc8746Hex(t, "d9041082820203d8414c000200040008000400100100")
	var fixed [2][3]uint16
	NewDecoderBytes(golden, h).MustDecode(&fixed)
	want := [2][3]uint16{{2, 8, 16}, {4, 4, 256}}
	if fixed != want {
		t.Fatalf("col-major fixed: %v want %v", fixed, want)
	}
	var nested [][]uint16
	NewDecoderBytes(golden, h).MustDecode(&nested)
	if !reflect.DeepEqual(nested, [][]uint16{{2, 8, 16}, {4, 4, 256}}) {
		t.Fatalf("col-major nested: %v", nested)
	}

	// 3D column-major: dims [2][2][2], first dim contiguous
	// order: (0,0,0)=1,(1,0,0)=2,(0,1,0)=3,(1,1,0)=4,(0,0,1)=5,(1,0,1)=6,(0,1,1)=7,(1,1,1)=8
	g3 := rfc8746Hex(t,
		"d904108283020202d84550"+"01000200030004000500060007000800")
	var a3 [2][2][2]int16
	NewDecoderBytes(g3, h).MustDecode(&a3)
	w3 := [2][2][2]int16{{{1, 5}, {3, 7}}, {{2, 6}, {4, 8}}}
	if a3 != w3 {
		t.Fatalf("3d col-major: %v want %v", a3, w3)
	}
}

func TestCborRfc8746HomogeneousTag41(t *testing.T) {
	h := rfc8746Handle()
	// tag41 wrapping a classical array [1,2]
	bs := rfc8746Hex(t, "d829820102")
	var v []int16
	NewDecoderBytes(bs, h).MustDecode(&v)
	if !reflect.DeepEqual(v, []int16{1, 2}) {
		t.Fatalf("tag41: %v", v)
	}
	// tag41 inside tag40 (classical content)
	bs = rfc8746Hex(t, "d82882820202d8298401020304")
	var m [2][2]int32
	NewDecoderBytes(bs, h).MustDecode(&m)
	if m != [2][2]int32{{1, 2}, {3, 4}} {
		t.Fatalf("tag40+41: %v", m)
	}
}

func TestCborRfc8746InterfaceNaked(t *testing.T) {
	h := rfc8746Handle()
	var v interface{}
	NewDecoderBytes(rfc8746Hex(t, "d84e480100000002000000"), h).MustDecode(&v)
	iv, ok := v.([]int32)
	if !ok || !reflect.DeepEqual(iv, []int32{1, 2}) {
		t.Fatalf("naked 1d: %#v", v)
	}

	var m interface{}
	NewDecoderBytes(rfc8746Hex(t, "d82882820203d8414c000200040008000400100100"), h).MustDecode(&m)
	im, ok := m.([][]uint16)
	if !ok || !reflect.DeepEqual(im, [][]uint16{{2, 4, 8}, {4, 16, 256}}) {
		t.Fatalf("naked md: %#v", m)
	}
}

func TestCborRfc8746Canonical(t *testing.T) {
	h := rfc8746Handle()
	h.Canonical = true
	v := [][]int64{{1, 2, 3}, {4, 5, 6}}
	b1 := rfc8746EncBytes(t, h, &v)
	b2 := rfc8746EncIO(t, h, &v)
	if !bytes.Equal(b1, b2) {
		t.Fatalf("non-deterministic: %x vs %x", b1, b2)
	}
	b3 := rfc8746EncBytes(t, h, &v)
	if !bytes.Equal(b1, b3) {
		t.Fatalf("non-deterministic across calls")
	}
	// definite outer containers: no indefinite bytes anywhere
	for _, b := range b1 {
		if b == 0x9f || b == 0x5f || b == 0xff {
			t.Fatalf("indefinite encoding in canonical: %x", b1)
		}
	}
}

func TestCborRfc8746DisabledByDefault(t *testing.T) {
	h := new(CborHandle) // TypedArrays off
	v := []int16{1, 2}
	bs := rfc8746EncBytes(t, h, &v)
	if bs[0] != 0x82 {
		t.Fatalf("classical array expected, got %x", bs)
	}
	var v2 []int16
	NewDecoderBytes(bs, h).MustDecode(&v2)
	if !reflect.DeepEqual(v, v2) {
		t.Fatalf("%v", v2)
	}
}

func TestCborRfc8746Malformed(t *testing.T) {
	h := rfc8746Handle()

	// reserved tag 76
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84c40"), h, new([]int8))
	// float128 tags 83/87
	rfc8746ExpectErr(t, rfc8746Hex(t, "d853500000000000000000000000000000000000"), h, new([]float64))
	rfc8746ExpectErr(t, rfc8746Hex(t, "d857500000000000000000000000000000000000"), h, new([]float64))
	// truncated byte string
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84e44010000"), h, new([]int32))
	// byte length not a multiple of element size
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84e43010000"), h, new([]int32))
	// content is not a byte string
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84e820102"), h, new([]int32))
	// indefinite byte string rejected
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84e5f4101ff"), h, new([]int32))
	// overflowing length header (uint64 max): must error, never allocate
	rfc8746ExpectErr(t, rfc8746Hex(t, "d8475bffffffffffffffff"), h, new([]uint64))

	// multi-dimensional: outer array length != 2
	rfc8746ExpectErr(t, rfc8746Hex(t, "d82883820203d8414c000200040008000400100100"), h, new([2][3]uint16))
	// dims not an array
	rfc8746ExpectErr(t, rfc8746Hex(t, "d8288202d84140"), h, new([2]uint16))
	// zero dimension
	rfc8746ExpectErr(t, rfc8746Hex(t, "d82882820003d84140"), h, new([0][3]uint16))
	// negative dimension
	rfc8746ExpectErr(t, rfc8746Hex(t, "d82882822003d84140"), h, new([2][3]uint16))
	// dims product does not match content byte length
	rfc8746ExpectErr(t, rfc8746Hex(t, "d82882820203d8414a00020004000800040010"), h, new([2][3]uint16))
	// classical content length mismatch
	rfc8746ExpectErr(t, rfc8746Hex(t, "d8288282020283010203"), h, new([2][2]int))
	// fixed-array shape mismatch (2x3 vs 3x2)
	rfc8746ExpectErr(t, rfc8746Hex(t, "d82882820203d8414c000200040008000400100100"), h, new([3][2]uint16))
	// huge dimension value
	rfc8746ExpectErr(t, rfc8746Hex(t, "d82882821bffffffffffffff7fd84140"), h, new([][]uint16))
	// content is neither bstr nor array
	rfc8746ExpectErr(t, rfc8746Hex(t, "d8288282020301"), h, new([2][3]uint16))

	// sign/width incompatibilities
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84f48ffffffffffffffff"), h, new([]uint64))  // -1 into uint
	rfc8746ExpectErr(t, rfc8746Hex(t, "d847480000000000000001"), h, new([]uint32))  // uint64 into uint32
	rfc8746ExpectErr(t, rfc8746Hex(t, "d84e480000000000000000"), h, new([]float64)) // int tag into float
	rfc8746ExpectErr(t, rfc8746Hex(t, "d856480000000000000000"), h, new([]float32)) // f64 into f32
	rfc8746ExpectErr(t, rfc8746Hex(t, "d855440000803f"), h, new([]int16))           // f32 into int
}

func TestCborRfc8746MalformedNoPanic(t *testing.T) {
	h := rfc8746Handle()
	// random/truncated streams starting like typed arrays must return
	// errors instead of panicking through both drivers.
	garbage := [][]byte{
		rfc8746Hex(t, "d8"),
		rfc8746Hex(t, "d841"),
		rfc8746Hex(t, "d8415b01"),
		rfc8746Hex(t, "d828"),
		rfc8746Hex(t, "d82882820203d841"),
		rfc8746Hex(t, "d9041082820203d8414c00"),
	}
	decodeErr := func(bs []byte, v interface{}) {
		t.Helper()
		if err := NewDecoderBytes(bs, h).Decode(v); err == nil {
			t.Fatalf("expected error decoding %x into %T", bs, v)
		}
		if err := NewDecoder(bytes.NewReader(bs), h).Decode(v); err == nil {
			t.Fatalf("expected error (io) decoding %x into %T", bs, v)
		}
	}
	for i, bs := range garbage {
		var v []int16
		decodeErr(bs, &v)
		var m [][]int16
		decodeErr(bs, &m)
	}
}
