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

// Package directive parses //memsafe: comments, once per package, for every check to share.
//
// Of the directive language, only allow is enforced today:
//
//	//memsafe:allow MS301[,MS302] <reason>
//
// suppresses the named checks on its own line and on the next line, and requires a reason.
// Every other verb in the language is recognised but not implemented yet, and is reported as
// MS000 so that a contract nobody checks never looks checked. Unknown verbs, unknown check
// IDs, and missing reasons are reported as MS000 too. No directive is ever silently ignored.
//
// To implement a verb, remove it from pendingVerbs, parse its arguments in parse, and expose
// what checks need through Result.
package directive

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

const prefix = "//memsafe:"

// The verbs of the directive language.
const (
	verbAllow       = "allow"
	verbStrict      = "strict"
	verbForbid      = "forbid"
	verbOwned       = "owned"
	verbConsumes    = "consumes"
	verbBorrows     = "borrows"
	verbViewOf      = "viewof"
	verbInvalidates = "invalidates"
)

// Analyzer parses directives and reports malformed ones.
var Analyzer = &analysis.Analyzer{
	Name:       "directive",
	Doc:        "reports malformed //memsafe: directives (MS000)",
	Run:        run,
	ResultType: reflect.TypeFor[*Result](),
}

// pendingVerbs are valid directives whose checks are not implemented yet.
var pendingVerbs = map[string]bool{
	verbStrict: true, verbForbid: true, verbOwned: true, verbConsumes: true,
	verbBorrows: true, verbViewOf: true, verbInvalidates: true,
}

// Result answers whether a diagnostic is suppressed.
type Result struct {
	// allowed maps file name and line to the check IDs allowed there.
	allowed map[lineKey][]string
}

type lineKey struct {
	file string
	line int
}

// Suppressed reports whether a //memsafe:allow directive on the same line as pos, or on the
// line above it, names id.
func (r *Result) Suppressed(fset *token.FileSet, pos token.Pos, id string) bool {
	p := fset.Position(pos)
	for _, line := range []int{p.Line, p.Line - 1} {
		if slices.Contains(r.allowed[lineKey{p.Filename, line}], id) {
			return true
		}
	}

	return false
}

func run(pass *analysis.Pass) (any, error) {
	res := &Result{allowed: make(map[lineKey][]string)}
	report := !vetmode.FactsOnly()

	for _, file := range pass.Files {
		for _, group := range file.Comments {
			for _, c := range group.List {
				if strings.HasPrefix(c.Text, prefix) {
					parse(pass, res, c, report)
				}
			}
		}
	}

	return res, nil
}

func parse(pass *analysis.Pass, res *Result, c *ast.Comment, report bool) {
	malformed := func(format string, args ...any) {
		if report {
			pass.Report(analysis.Diagnostic{Pos: c.Pos(), End: c.End(), Category: string(catalog.MalformedDirective),
				Message: string(catalog.MalformedDirective) + ": " + fmt.Sprintf(format, args...)})
		}
	}

	verb, rest, _ := strings.Cut(strings.TrimPrefix(c.Text, prefix), " ")
	switch {
	case verb == verbAllow:
		ids, reason, _ := strings.Cut(strings.TrimSpace(rest), " ")
		if ids == "" {
			malformed("//memsafe:allow must name the check IDs it suppresses")
			return
		}

		var named []string

		for id := range strings.SplitSeq(ids, ",") {
			if !catalog.IsKnown(id) {
				malformed("unknown check ID %q in //memsafe:allow", id)
				return
			}

			named = append(named, id)
		}

		if strings.TrimSpace(reason) == "" {
			malformed("suppression requires a reason: //memsafe:allow %s <reason>", ids)
			return
		}

		p := pass.Fset.Position(c.Pos())
		key := lineKey{p.Filename, p.Line}
		res.allowed[key] = append(res.allowed[key], named...)
	case pendingVerbs[verb]:
		malformed("//memsafe:%s is not supported yet by this version of gomemsafe", verb)
	default:
		malformed("unknown directive //memsafe:%s", verb)
	}
}
