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

// Package vetmode answers two questions about how the go command invoked the binary: is this
// a vet-tool invocation at all, and does this run only compute facts for a dependency (a
// VetxOnly run)?
//
// The second matters for speed. unitchecker also runs, in facts-only runs, every analyzer
// that requires a fact producer, and every check requires summary. Checks therefore call
// FactsOnly and return immediately, so dependencies pay only for summaries
// (docs/design.md §7.2). A test in package frontend fails if unitchecker stops doing this,
// which is the signal to drop the short-circuit.
package vetmode

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// Arguments the go command passes to a vet tool.
const (
	flagsQuery    = "-flags"
	versionFlag   = "-V"
	configSuffix  = ".cfg"
	versionPrefix = versionFlag + "="
)

var factsOnly atomic.Bool

// FactsOnly reports whether this process only computes facts.
func FactsOnly() bool { return factsOnly.Load() }

// Set records the mode. Tests use it directly; the binary uses Detect.
func Set(onlyFacts bool) { factsOnly.Store(onlyFacts) }

// IsVetToolInvocation reports whether args come from the go command driving a vet tool:
// a version or flags query, or a run on a vet.cfg file.
func IsVetToolInvocation(args []string) bool {
	for _, a := range args {
		if a == flagsQuery || a == versionFlag || strings.HasPrefix(a, versionPrefix) {
			return true
		}
	}

	return len(args) > 0 && strings.HasSuffix(args[len(args)-1], configSuffix)
}

// Detect reads the vet.cfg file named by the last argument, if any, and records its VetxOnly
// setting.
func Detect(args []string) error {
	if len(args) == 0 || !strings.HasSuffix(args[len(args)-1], configSuffix) {
		return nil
	}

	data, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		return fmt.Errorf("reading vet config: %w", err)
	}

	var cfg struct{ VetxOnly bool }
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("decoding vet config: %w", err)
	}

	Set(cfg.VetxOnly)

	return nil
}
