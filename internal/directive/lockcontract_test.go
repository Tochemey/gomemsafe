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

package directive_test

import (
	"testing"

	"github.com/tochemey/gomemsafe/internal/directive"
)

// TestLockContractsCompareByWhatTheyDeclare checks Equal and Empty: a nil contract equals an
// empty one, any difference in a reference, its mode, its path, or the ignore flag tells
// contracts apart, and the entry and exit sets are the required locks plus the released or
// acquired ones.
func TestLockContractsCompareByWhatTheyDeclare(t *testing.T) {
	ref := func(name string, exclusive bool, path ...int) directive.LockRef {
		return directive.LockRef{Name: name, Kind: directive.LockParam, Path: path, Exclusive: exclusive}
	}

	base := &directive.LockContract{
		Requires: []directive.LockRef{ref("t.mu", true, 0)},
		Acquires: []directive.LockRef{ref("t.other", true, 1)},
		Releases: []directive.LockRef{ref("t.third", true, 2)},
		Excludes: []directive.LockRef{ref("g", false)},
	}

	same := &directive.LockContract{
		Requires: []directive.LockRef{ref("t.mu", true, 0)},
		Acquires: []directive.LockRef{ref("t.other", true, 1)},
		Releases: []directive.LockRef{ref("t.third", true, 2)},
		Excludes: []directive.LockRef{ref("g", false)},
	}

	if !base.Equal(same) || !same.Equal(base) || base.Empty() {
		t.Fatal("equal contracts compare unequal, or a declaring contract is empty")
	}

	for name, other := range map[string]*directive.LockContract{
		"mode":    {Requires: []directive.LockRef{ref("t.mu", false, 0)}, Acquires: base.Acquires, Releases: base.Releases, Excludes: base.Excludes},
		"path":    {Requires: []directive.LockRef{ref("t.mu", true, 3)}, Acquires: base.Acquires, Releases: base.Releases, Excludes: base.Excludes},
		"global":  {Requires: []directive.LockRef{{Name: "t.mu", Kind: directive.LockGlobal, Global: "p.mu", Exclusive: true}}, Acquires: base.Acquires, Releases: base.Releases, Excludes: base.Excludes},
		"missing": {Requires: base.Requires, Acquires: base.Acquires, Releases: base.Releases},
		"ignore":  {Requires: base.Requires, Acquires: base.Acquires, Releases: base.Releases, Excludes: base.Excludes, Ignore: true},
		"empty":   {},
		"nil":     nil,
	} {
		if base.Equal(other) || other.Equal(base) {
			t.Errorf("%s: contracts compare equal", name)
		}
	}

	var none *directive.LockContract

	if !none.Equal(&directive.LockContract{}) || !(&directive.LockContract{}).Equal(none) || !none.Empty() {
		t.Error("a nil contract is not equal to an empty one")
	}

	if none.Equal(&directive.LockContract{Ignore: true}) {
		t.Error("a nil contract equals an ignoring one")
	}

	if entry, exit := base.HeldOnEntry(), base.HeldOnExit(); len(entry) != 2 || entry[1].Name != "t.third" ||
		len(exit) != 2 || exit[1].Name != "t.other" {
		t.Errorf("HeldOnEntry = %v, HeldOnExit = %v", entry, exit)
	}
}
