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

// Package user calls helper, so MS301 relies on helper's exported summaries.
package user

import "helper"

func afterRelease() int {
	buf := helper.Get()
	helper.Release(buf)

	return buf.Len() // want `MS301: buf used after the call to helper.Release invalidated it`
}

func afterTransitiveRelease() int {
	buf := helper.Get()
	helper.ReleaseTwice(buf)

	return buf.Len() // want `MS301: buf used after the call to helper.ReleaseTwice invalidated it`
}

func afterMethodRelease() int {
	var o helper.Owner

	buf := helper.Get()
	o.Close(buf)

	return buf.Len() // want `MS301: buf used after the call to \(\*helper.Owner\).Close invalidated it`
}

func afterMaybeRelease(c bool) int {
	buf := helper.Get()
	helper.MaybeRelease(c, buf)

	return buf.Len()
}

func afterSelfRelease() int {
	p := helper.GetPooled()
	p.Release()

	return p.Len() // want `MS301: p used after the call to \(\*helper.Pooled\).Release invalidated it`
}

// afterFree mirrors zap: Buffer.Free returns the buffer through a generic pool wrapper.
func afterFree() int {
	buf := helper.NewBuffer()
	buf.Free()

	return buf.Len() // want `MS301: buf used after the call to \(\*helper.Buffer\).Free invalidated it`
}

func afterRecycle() int {
	buf := helper.Get()
	helper.Recycle(buf)

	return buf.Len() // want `MS301: buf used after the call to helper.Recycle invalidated it`
}

// recycledCopy passes an int to the same generic function, which copies it.
func recycledCopy(n int) int {
	helper.Recycle(n)

	return n
}
