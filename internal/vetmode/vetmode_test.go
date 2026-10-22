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

package vetmode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsVetToolInvocation(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"-flags"}, true},
		{[]string{"-V=full"}, true},
		{[]string{"-V"}, true},
		{[]string{"-lifetime", "/tmp/b001/vet.cfg"}, true},
		{[]string{"./..."}, false},
		{[]string{"-json", "./..."}, false},
		{nil, false},
	}

	for _, tt := range tests {
		if got := IsVetToolInvocation(tt.args); got != tt.want {
			t.Errorf("IsVetToolInvocation(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestDetectReadsVetxOnly(t *testing.T) {
	t.Cleanup(func() { Set(false) })

	for _, want := range []bool{true, false} {
		cfg := writeConfig(t, map[bool]string{true: `{"VetxOnly": true}`, false: `{"VetxOnly": false}`}[want])

		if err := Detect([]string{cfg}); err != nil {
			t.Fatal(err)
		}

		if FactsOnly() != want {
			t.Fatalf("FactsOnly() = %v after a config with VetxOnly %v", FactsOnly(), want)
		}
	}
}

func TestDetectIgnoresArgsWithoutConfig(t *testing.T) {
	Set(true)
	t.Cleanup(func() { Set(false) })

	if err := Detect([]string{"-flags"}); err != nil {
		t.Fatal(err)
	}

	if err := Detect(nil); err != nil {
		t.Fatal(err)
	}

	if !FactsOnly() {
		t.Fatal("Detect changed the mode without a config file")
	}
}

func TestDetectReportsBadConfigs(t *testing.T) {
	if err := Detect([]string{filepath.Join(t.TempDir(), "missing.cfg")}); err == nil {
		t.Error("missing config: no error")
	}

	if err := Detect([]string{writeConfig(t, "{not json")}); err == nil {
		t.Error("malformed config: no error")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	name := filepath.Join(t.TempDir(), "vet.cfg")
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return name
}
