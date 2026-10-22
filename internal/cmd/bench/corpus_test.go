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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCorpusAcceptsThePinnedCorpus(t *testing.T) {
	entries, err := loadCorpus(corpusFileName)
	if err != nil {
		t.Fatal(err)
	}

	repos := 0

	for _, e := range entries {
		if e.Repo == "" {
			continue
		}

		repos++

		if len(e.Commit) != 40 {
			t.Errorf("%s: commit %q is not a full SHA", e.Name, e.Commit)
		}
	}

	if repos != 1 {
		t.Fatalf("the corpus pins %d repositories, want exactly one small library", repos)
	}
}

func TestLoadCorpusRejectsInvalidEntries(t *testing.T) {
	tests := map[string]string{
		"no name":          `[{"patterns": ["./..."], "synthetic": true}]`,
		"no patterns":      `[{"name": "a", "synthetic": true}]`,
		"two sources":      `[{"name": "a", "repo": "https://x", "commit": "c", "synthetic": true, "patterns": ["./..."]}]`,
		"no source":        `[{"name": "a", "patterns": ["./..."]}]`,
		"unpinned repo":    `[{"name": "a", "repo": "https://x", "patterns": ["./..."]}]`,
		"duplicate names":  `[{"name": "a", "synthetic": true, "patterns": ["./..."]}, {"name": "a", "synthetic": true, "patterns": ["./..."]}]`,
		"malformed JSON":   `[{`,
		"name with a path": `[{"name": "../a", "synthetic": true, "patterns": ["./..."]}]`,
		"escaping dir":     `[{"name": "a", "repo": "https://x", "commit": "c", "dir": "../b", "patterns": ["./..."]}]`,
		"repo as option":   `[{"name": "a", "repo": "--upload-pack=x", "commit": "c", "patterns": ["./..."]}]`,
	}

	for name, content := range tests {
		path := filepath.Join(t.TempDir(), corpusFileName)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := loadCorpus(path); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	if _, err := loadCorpus(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file: no error")
	}
}

func TestSelectEntries(t *testing.T) {
	all := []Entry{
		{Name: "zap", Repo: "r", Commit: "c", Patterns: []string{"./..."}},
		{Name: syntheticEntry, Synthetic: true, Patterns: []string{"./..."}},
	}

	if got, _ := selectEntries(all, nil, false); len(got) != 2 {
		t.Fatalf("no names: %d entries, want all", len(got))
	}

	got, err := selectEntries(all, []string{"zap"}, false)
	if err != nil || len(got) != 1 || got[0].Name != "zap" || got[0].Small {
		t.Fatalf("by name: %+v, %v", got, err)
	}

	got, err = selectEntries(all, []string{"zap"}, true)
	if err != nil || len(got) != 1 || got[0].Name != syntheticEntry || !got[0].Small {
		t.Fatalf("short: %+v, %v; want only the small generated corpus", got, err)
	}

	if _, err := selectEntries(all, []string{"nope"}, false); err == nil {
		t.Fatal("unknown name: no error")
	}
}

func TestEntryCloneName(t *testing.T) {
	if got := (Entry{Name: "zap", Commit: "abc"}).cloneName(); got != "zap@abc" {
		t.Fatalf("cloneName = %q", got)
	}
}
