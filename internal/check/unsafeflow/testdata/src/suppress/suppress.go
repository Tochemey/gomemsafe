// MIT License
//
// Copyright (c) 2026 Arsene Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package suppress silences MS101, MS102, and MS103 with a reason.
package suppress

import (
	"reflect"
	"unsafe"
)

func allowed(u uintptr) unsafe.Pointer {
	//memsafe:allow MS101 u is an address the C library keeps alive
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

func allowedHeader(data uintptr, n int) []byte {
	h := reflect.SliceHeader{Data: data, Len: n, Cap: n}

	//memsafe:allow MS103 data is C memory that the caller frees after the last use
	return *(*[]byte)(unsafe.Pointer(&h))
}

func allowedMutation(b []byte) string {
	s := unsafe.String(&b[0], len(b))
	b[0] = 'x'

	//memsafe:allow MS102 s is a scratch key that nothing keeps
	return s
}
