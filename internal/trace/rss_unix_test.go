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

//go:build unix

package trace

import (
	"syscall"
	"testing"
)

func TestMaxRSSIsMeasured(t *testing.T) {
	if got := maxRSS(); got <= 0 {
		t.Fatalf("maxRSS() = %d, want a positive byte count", got)
	}
}

func TestRSSBytesConvertsPerPlatform(t *testing.T) {
	for goos, want := range map[string]int64{"darwin": 7, "ios": 7, "linux": 7 * 1024, "freebsd": 7 * 1024} {
		if got := rssBytes(7, goos); got != want {
			t.Errorf("rssBytes(7, %q) = %d, want %d", goos, got, want)
		}
	}
}

func TestRusageMaxRSSUsesTheRecordedPeak(t *testing.T) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}

	if got := RusageMaxRSS(&ru); got <= 0 {
		t.Fatalf("RusageMaxRSS = %d, want a positive byte count", got)
	}
}
