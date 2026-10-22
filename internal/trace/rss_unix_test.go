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
	"errors"
	"syscall"
	"testing"
)

func TestMaxRSSIsMeasured(t *testing.T) {
	if got := maxRSS(); got <= 0 {
		t.Fatalf("maxRSS() = %d, want a positive byte count", got)
	}
}

func TestPeakRSSReadsVMHWMOnLinux(t *testing.T) {
	status := func(string) ([]byte, error) { return []byte("Name:\tgomemsafe\nVmHWM:\t   4096 kB\n"), nil }
	if got := peakRSS("linux", status); got != 4096*1024 {
		t.Fatalf("peakRSS on linux = %d, want VmHWM in bytes (%d)", got, 4096*1024)
	}

	missing := func(string) ([]byte, error) { return nil, errors.New("no /proc") }
	if got := peakRSS("linux", missing); got <= 0 {
		t.Fatalf("peakRSS without /proc = %d, want the getrusage fallback", got)
	}

	if got := peakRSS("darwin", status); got <= 0 || got == 4096*1024 {
		t.Fatalf("peakRSS on darwin = %d, want getrusage, not VmHWM", got)
	}
}

func TestParseVMHWM(t *testing.T) {
	tests := []struct {
		status string
		want   int64
		ok     bool
	}{
		{"VmPeak:\t 9 kB\nVmHWM:\t 12 kB\nVmRSS:\t 3 kB\n", 12 * 1024, true},
		{"VmHWM:\t 12 MB\n", 0, false},
		{"VmHWM:\t lots kB\n", 0, false},
		{"VmRSS:\t 3 kB\n", 0, false},
	}

	for _, tt := range tests {
		if got, ok := parseVMHWM([]byte(tt.status)); got != tt.want || ok != tt.ok {
			t.Errorf("parseVMHWM(%q) = %d, %v; want %d, %v", tt.status, got, ok, tt.want, tt.ok)
		}
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
