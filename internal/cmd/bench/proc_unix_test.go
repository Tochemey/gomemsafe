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

package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCancelKillsTheWholeProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// The shell starts a child and prints its pid; the child must die with the shell.
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $!; wait")
	stopGroupOnCancel(cmd)

	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 32)

	n, err := out.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	child, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	cancel()

	_ = cmd.Wait()

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Wait took %v after cancel, want the group killed at once", elapsed)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(child, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still runs after the group was killed", child)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
