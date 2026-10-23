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

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSyntheticBuilds(t *testing.T) {
	dir, err := generateSynthetic(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "p*", "*.go"))
	if err != nil || len(files) != smallSyntheticPackages {
		t.Fatalf("generated %d package files, %v; want %d", len(files), err, smallSyntheticPackages)
	}

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated corpus does not build: %v\n%s", err, out)
	}
}

func TestSyntheticPackageChainsImports(t *testing.T) {
	first, second := syntheticPackage(0, 1), syntheticPackage(1, 2)

	if strings.Contains(first, syntheticModule+"/") || !strings.Contains(first, "Release(buf)") {
		t.Fatalf("p0 must import nothing generated and release locally:\n%s", first)
	}

	if !strings.Contains(second, `"`+syntheticModule+`/p0"`) || !strings.Contains(second, "p0.Release(buf)") ||
		!strings.Contains(second, "func F1(") {
		t.Fatalf("p1 must import p0, release through it, and have two functions:\n%s", second)
	}
}

func TestGenerateSyntheticFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")

	if _, err := generateSynthetic(filepath.Join(file, "corpus"), true); err == nil {
		t.Error("a corpus directory under a file: no error")
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, packageName(0)), "")

	if _, err := generateSynthetic(dir, true); err == nil {
		t.Error("a file where a package directory goes: no error")
	}
}
