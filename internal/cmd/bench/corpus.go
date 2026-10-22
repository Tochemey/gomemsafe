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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// syntheticEntry is the corpus entry -short keeps: the generated corpus, which needs no
// network.
const syntheticEntry = "synthetic"

// Entry is one codebase of the benchmark corpus: a repository pinned by commit, or the
// generated corpus (synthetic.go). Exactly one of Repo and Synthetic is set.
type Entry struct {
	Name string `json:"name"`
	// Repo, Tag, and Commit pin a git repository. Commit is authoritative: a clone whose HEAD
	// differs is an error.
	Repo   string `json:"repo,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Commit string `json:"commit,omitempty"`
	// Dir is the module directory inside the repository, when it is not the root.
	Dir string `json:"dir,omitempty"`
	// Synthetic analyses a generated worst case (synthetic.go).
	Synthetic bool `json:"synthetic,omitempty"`
	// Patterns are the package patterns passed to go vet and go build.
	Patterns []string `json:"patterns"`
	// Small asks the synthetic generator for a small corpus. -short sets it on every entry.
	Small bool `json:"small,omitempty"`
}

// loadCorpus reads and validates the corpus file.
func loadCorpus(path string) ([]Entry, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	seen := make(map[string]bool)

	for _, e := range entries {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		if seen[e.Name] {
			return nil, fmt.Errorf("%s: entry %q appears twice", path, e.Name)
		}

		seen[e.Name] = true
	}

	return entries, nil
}

// selectEntries keeps the named entries (all when names is empty). With short, it keeps only
// a small generated corpus: the smoke run.
func selectEntries(all []Entry, names []string, short bool) ([]Entry, error) {
	if short {
		names = []string{syntheticEntry}
	}

	if len(names) == 0 {
		return all, nil
	}

	var out []Entry

	for _, name := range names {
		i := slices.IndexFunc(all, func(e Entry) bool { return e.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no corpus entry named %q", name)
		}

		e := all[i]
		e.Small = e.Small || short

		out = append(out, e)
	}

	return out, nil
}

// cloneName is the directory a pinned repository is cloned into, named after the pin.
func (e Entry) cloneName() string {
	return e.Name + "@" + e.Commit
}

func (e Entry) validate() error {
	if e.Name == "" {
		return errors.New("an entry has no name")
	}

	// Names and directories become paths inside the run's temporary directory, and the
	// repository an argument of git: none may escape or read as an option.
	if !filepath.IsLocal(e.Name) || strings.ContainsAny(e.Name, `/\`) {
		return fmt.Errorf("entry name %q must be a plain file name", e.Name)
	}

	if e.Dir != "" && !filepath.IsLocal(e.Dir) {
		return fmt.Errorf("entry %q: dir %q must stay inside the repository", e.Name, e.Dir)
	}

	if strings.HasPrefix(e.Repo, "-") {
		return fmt.Errorf("entry %q: repo %q is not a repository", e.Name, e.Repo)
	}

	if len(e.Patterns) == 0 {
		return fmt.Errorf("entry %q has no patterns", e.Name)
	}

	if (e.Repo != "") == e.Synthetic {
		return fmt.Errorf("entry %q must set exactly one of repo and synthetic", e.Name)
	}

	if e.Repo != "" && (e.Commit == "" || strings.ContainsAny(e.Commit, " /")) {
		return fmt.Errorf("entry %q must pin a commit", e.Name)
	}

	return nil
}
