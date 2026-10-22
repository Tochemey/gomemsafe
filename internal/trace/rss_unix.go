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
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Where Linux reports a process's own peak resident set size: the VmHWM line of
// /proc/self/status, in kilobytes.
const (
	procStatus  = "/proc/self/status"
	vmHWMPrefix = "VmHWM:"
	kilobytes   = "kB"
)

// RusageMaxRSS returns the peak resident set size recorded in ru, in bytes. On Linux it also
// counts the peak of the process that started this one: the kernel carries the parent's
// high-water mark across exec.
func RusageMaxRSS(ru *syscall.Rusage) int64 { return rssBytes(ru.Maxrss, runtime.GOOS) }

// maxRSS returns the peak resident set size of this process in bytes.
func maxRSS() int64 { return peakRSS(runtime.GOOS, os.ReadFile) }

// peakRSS returns this process's own peak resident set size. On Linux it reads VmHWM, because
// getrusage would report the go command's peak for a small vet tool the go command started.
// Elsewhere, or when VmHWM cannot be read, it falls back to getrusage.
func peakRSS(goos string, readFile func(string) ([]byte, error)) int64 {
	if goos == "linux" {
		if status, err := readFile(procStatus); err == nil {
			if hwm, ok := parseVMHWM(status); ok {
				return hwm
			}
		}
	}

	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}

	return RusageMaxRSS(&ru)
}

// rssBytes converts getrusage's maxrss to bytes: Apple platforms report bytes, the other
// Unix systems kilobytes.
func rssBytes(maxrss int64, goos string) int64 {
	if goos == "darwin" || goos == "ios" {
		return maxrss
	}

	return maxrss * 1024
}

// parseVMHWM returns the VmHWM value of a /proc/<pid>/status file in bytes.
func parseVMHWM(status []byte) (int64, bool) {
	for line := range strings.Lines(string(status)) {
		value, ok := strings.CutPrefix(line, vmHWMPrefix)
		if !ok {
			continue
		}

		fields := strings.Fields(value)
		if len(fields) != 2 || fields[1] != kilobytes {
			return 0, false
		}

		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, false
		}

		return kb * 1024, true
	}

	return 0, false
}
