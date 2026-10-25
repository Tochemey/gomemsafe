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
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// suppressed reports, for the declaration of x in the fixture, which IDs are suppressed on
// its line.
var suppressed = &analysis.Analyzer{
	Name:     "directivesuppressed",
	Doc:      "reports suppressions for tests",
	Requires: []*analysis.Analyzer{directive.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		res := pass.ResultOf[directive.Analyzer].(*directive.Result)

		for _, file := range pass.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}

				pos := gen.Pos()
				pass.Reportf(pos, "MS301=%v MS302=%v MS303=%v",
					res.Suppressed(pass.Fset, pos, "MS301"),
					res.Suppressed(pass.Fset, pos, "MS302"),
					res.Suppressed(pass.Fset, pos, "MS303"))
			}
		}

		return nil, nil
	},
}

func TestReportsMalformedDirectives(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "d")
}

func TestReportsNothingInFactsOnlyRuns(t *testing.T) {
	vetmode.Set(true)
	t.Cleanup(func() { vetmode.Set(false) })

	results := analysistest.Run(&silentT{t}, analysistest.TestData(), directive.Analyzer, "d")
	for _, r := range results {
		if len(r.Diagnostics) != 0 {
			t.Fatalf("got %d diagnostics in a facts-only run, want 0", len(r.Diagnostics))
		}
	}
}

// TestContractsGiveArgumentIndexes checks the contracts of the well-formed viewof and
// invalidates directives in the fixture: argument indexes count the receiver as 0, a declared
// view is ended by every event, and a malformed directive adds nothing.
func TestContractsGiveArgumentIndexes(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "d")[0]
	res := r.Result.(*directive.Result)

	view := func(arg int) effect.View { return effect.View{Arg: arg, EndedBy: effect.All} }
	want := []struct {
		name           string
		view           effect.View
		ends, consumes effect.ArgSet
	}{
		{"Bytes", view(0), 0, 0},
		{"Reset", effect.View{}, 1 << 0, 0},
		{"Swap", view(2), 1<<2 | 1<<3, 0},
		{"Unnamed", view(1), 0, 0},
		{"Twice", view(0), 0, 0},
		{"Consume", effect.View{}, 0, 1 << 1},
	}

	if len(res.Contracts) != len(want) {
		t.Fatalf("%d contracts, want %d", len(res.Contracts), len(want))
	}

	for i, w := range want {
		c := res.Contracts[i]
		if c.Func.Name() != w.name || c.View != w.view || c.Ends != w.ends || c.Consumes != w.consumes {
			t.Errorf("contract %d = %s %+v %#x %#x, want %s %+v %#x %#x", i, c.Func.Name(), c.View,
				uint64(c.Ends), uint64(c.Consumes), w.name, w.view, uint64(w.ends), uint64(w.consumes))
		}

		if got, ok := res.ContractOf(c.Func); !ok || got != c {
			t.Errorf("ContractOf(%s) = %v, %v; want its contract", w.name, got, ok)
		}
	}

	if _, ok := res.ContractOf(r.Pass.Pkg.Scope().Lookup("Malformed").(*types.Func)); ok {
		t.Error("Malformed has a contract; its directives are all malformed")
	}
}

// TestOwnedTypesAreThoseWhoseDocCommentSaysSo checks the owned types of the fixture: a type
// declared alone or in a group with the directive in its own doc comment, and no other.
func TestOwnedTypesAreThoseWhoseDocCommentSaysSo(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "d")[0]
	res := r.Result.(*directive.Result)

	var names []string
	for _, tn := range res.Owned {
		names = append(names, tn.Name())
	}

	if strings.Join(names, ",") != "Frame,Packet" {
		t.Fatalf("Owned = %v, want Frame and Packet", names)
	}

	for name, want := range map[string]bool{"Frame": true, "Packet": true, "Plain": false, "First": false, "Argued": false} {
		if got := res.IsOwned(r.Pass.Pkg.Scope().Lookup(name).(*types.TypeName)); got != want {
			t.Errorf("IsOwned(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestSuppressedMatchesNamedIDsOnThatLine(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), suppressed, "s")
}

// FuzzDirective runs arbitrary text after //memsafe: through the directive analyzer. A
// directive is either a suppression of known check IDs or exactly one MS000 diagnostic: it is
// never silently ignored, and it never suppresses an ID outside the catalogue.
func FuzzDirective(f *testing.F) {
	for _, seed := range []string{
		"allow MS301 the buffer is copied first", "allow MS301,MS302 two checks", "allow",
		"allow MS301", "allow MS999 unknown", "allow ,, empty IDs", "allow MS301\ttab",
		"allow  MS301 two spaces", "owned", "strict", "unknownverb", "", "viewof x", "invalidates x", "consumes x", "owned x",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		// A line comment ends at the first line break.
		if strings.ContainsAny(text, "\n\r") {
			t.Skip("a directive is one line")
		}

		src := "package p\n\n//memsafe:" + text + "\nvar x int\n"

		fset := token.NewFileSet()

		file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
		if err != nil || len(file.Comments) != 1 || len(file.Decls) != 1 {
			t.Skip("the text does not form one line comment followed by a declaration")
		}

		var diags []analysis.Diagnostic

		pass := &analysis.Pass{
			Analyzer: directive.Analyzer, Fset: fset, Files: []*ast.File{file},
			Report: func(d analysis.Diagnostic) { diags = append(diags, d) },
		}

		res, err := directive.Analyzer.Run(pass)
		if err != nil {
			t.Fatal(err)
		}

		for _, d := range diags {
			if d.Category != string(catalog.MalformedDirective) {
				t.Fatalf("%q: diagnostic %q has category %q, want MS000", text, d.Message, d.Category)
			}
		}

		if len(diags) > 1 {
			t.Fatalf("%q: %d diagnostics for one directive", text, len(diags))
		}

		pos := file.Decls[0].Pos()
		suppressed := 0

		for _, id := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if !res.(*directive.Result).Suppressed(fset, pos, id) {
				continue
			}

			if !catalog.IsKnown(id) {
				t.Fatalf("%q: suppresses %q, which is not in the catalogue", text, id)
			}

			suppressed++
		}

		if len(diags) == 0 && suppressed == 0 {
			t.Fatalf("%q: neither reported nor a suppression", text)
		}
	})
}

// silentT discards analysistest's complaints about expectations, because in a facts-only run
// the fixture's expected diagnostics are deliberately absent.
type silentT struct{ *testing.T }

func (silentT) Errorf(string, ...any) {}
