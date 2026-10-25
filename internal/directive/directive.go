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
// Five verbs are enforced today. The first is allow:
//
//	//memsafe:allow MS301[,MS302] <reason>
//
// suppresses the named checks on its own line and on the next line, and requires a reason.
// Three declare a function's contract, in its doc comment:
//
//	//memsafe:viewof <param>
//	//memsafe:invalidates <param>
//	//memsafe:consumes <param>
//
// viewof says that the first result is a view into the parameter, valid until a call ends the
// parameter's views; invalidates says that a call ends every view of the parameter; consumes
// says that a call takes ownership of the argument, so the caller must not use it again.
// Result exposes them as a Contract per function, which the summary analyzer turns into
// facts. The last, in a type's doc comment, gives the type's values move semantics:
//
//	//memsafe:owned
//
// Every other verb in the language is recognised but not implemented yet, and is reported as
// MS000 so that a contract nobody checks never looks checked. Unknown verbs, unknown check
// IDs, missing reasons, and contracts that are misplaced or name no parameter are reported as
// MS000 too. No directive is ever silently ignored.
//
// To implement a verb, remove it from pendingVerbs, parse its arguments in parse, and expose
// what checks need through Result.
package directive

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/effect"
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
	verbStrict: true, verbForbid: true, verbBorrows: true,
}

// Result answers whether a diagnostic is suppressed, what contract a function declares, and
// which types are owned.
type Result struct {
	// Contracts lists, in source order, the functions whose doc comments declare a contract.
	Contracts []*Contract
	// Owned lists, in source order, the types whose doc comments declare them owned.
	Owned []*types.TypeName

	// allowed maps file name and line to the check IDs allowed there.
	allowed    map[lineKey][]string
	contractOf map[*types.Func]*Contract
	owned      map[*types.TypeName]bool
}

// Contract is what the //memsafe:viewof, //memsafe:invalidates, and //memsafe:consumes
// directives in a function's doc comment declare. Argument indexes count the receiver as 0,
// matching the argument order of an SSA call.
type Contract struct {
	Func *types.Func
	// View is the view the first result is, for viewof: into the named argument, and ended by
	// every event on it. It is zero when the function declares none.
	View effect.View
	// Ends lists the arguments every view of which a call ends, for invalidates.
	Ends effect.ArgSet
	// Consumes lists the arguments whose ownership a call takes, for consumes.
	Consumes effect.ArgSet
}

type lineKey struct {
	file string
	line int
}

// documented is the declaration a doc comment belongs to: a function, a type, or neither.
type documented struct {
	fn  *ast.FuncDecl
	typ *ast.TypeSpec
}

// ContractOf returns the contract that fn's doc comment declares, if it declares one.
func (r *Result) ContractOf(fn *types.Func) (*Contract, bool) {
	c, ok := r.contractOf[fn.Origin()]
	return c, ok
}

// IsOwned reports whether the doc comment of tn, a type of this package, declares it owned.
func (r *Result) IsOwned(tn *types.TypeName) bool { return r.owned[tn] }

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
	res := &Result{
		allowed: make(map[lineKey][]string), contractOf: make(map[*types.Func]*Contract),
		owned: make(map[*types.TypeName]bool),
	}
	report := !vetmode.FactsOnly()

	for _, file := range pass.Files {
		docs := docTargets(file)

		for _, group := range file.Comments {
			for _, c := range group.List {
				if strings.HasPrefix(c.Text, prefix) {
					parse(pass, res, c, docs[group], report)
				}
			}
		}
	}

	return res, nil
}

// docTargets maps each doc comment of file to the function or type declaration it documents.
// A type's doc comment is its spec's, or, in a declaration of one type without parentheses,
// the declaration's.
func docTargets(file *ast.File) map[*ast.CommentGroup]documented {
	docs := make(map[*ast.CommentGroup]documented)

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				docs[d.Doc] = documented{fn: d}
			}
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}

			if d.Doc != nil && !d.Lparen.IsValid() {
				docs[d.Doc] = documented{typ: d.Specs[0].(*ast.TypeSpec)}
			}

			for _, spec := range d.Specs {
				if ts := spec.(*ast.TypeSpec); ts.Doc != nil {
					docs[ts.Doc] = documented{typ: ts}
				}
			}
		}
	}

	return docs
}

// parse records the directive c, which is in the doc comment of doc's declaration.
func parse(pass *analysis.Pass, res *Result, c *ast.Comment, doc documented, report bool) {
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
	case verb == verbViewOf || verb == verbInvalidates || verb == verbConsumes:
		if msg := declareContract(pass, res, verb, strings.TrimSpace(rest), doc.fn); msg != "" {
			malformed("%s", msg)
		}
	case verb == verbOwned:
		if msg := declareOwned(pass, res, strings.TrimSpace(rest), doc.typ); msg != "" {
			malformed("%s", msg)
		}
	case pendingVerbs[verb]:
		malformed("//memsafe:%s is not supported yet by this version of gomemsafe", verb)
	default:
		malformed("unknown directive //memsafe:%s", verb)
	}
}

// declareContract adds the viewof, invalidates, or consumes directive of decl, naming param,
// to decl's contract. It returns what is wrong with the directive, or "" when it is
// well-formed.
func declareContract(pass *analysis.Pass, res *Result, verb, param string, decl *ast.FuncDecl) string {
	if decl == nil {
		return fmt.Sprintf("//memsafe:%s must be in the doc comment of a function or method declaration", verb)
	}

	if param == "" || strings.ContainsAny(param, " \t") {
		return fmt.Sprintf("//memsafe:%s must name one parameter of %s", verb, decl.Name.Name)
	}

	arg, ok := argIndex(decl, param)
	if !ok {
		return fmt.Sprintf("//memsafe:%s names %s, which is not a parameter of %s", verb, param, decl.Name.Name)
	}

	obj := pass.TypesInfo.Defs[decl.Name].(*types.Func)

	contract, seen := res.contractOf[obj]
	if !seen {
		contract = &Contract{Func: obj}
	}

	switch verb {
	case verbInvalidates:
		contract.Ends |= 1 << uint(arg)
	case verbConsumes:
		contract.Consumes |= 1 << uint(arg)
	default:
		switch {
		case decl.Type.Results.NumFields() == 0:
			return fmt.Sprintf("//memsafe:viewof needs a result to be the view, and %s returns nothing", decl.Name.Name)
		case contract.View != effect.View{}:
			return fmt.Sprintf("//memsafe:viewof declares the view of %s twice", decl.Name.Name)
		}

		contract.View = effect.View{Arg: arg, EndedBy: effect.All}
	}

	if !seen {
		res.contractOf[obj] = contract
		res.Contracts = append(res.Contracts, contract)
	}

	return ""
}

// declareOwned records the owned directive of spec, whose text after the verb is rest. It
// returns what is wrong with the directive, or "" when it is well-formed.
func declareOwned(pass *analysis.Pass, res *Result, rest string, spec *ast.TypeSpec) string {
	switch {
	case spec == nil:
		return "//memsafe:owned must be in the doc comment of a type declaration"
	case rest != "":
		return fmt.Sprintf("//memsafe:owned takes no arguments, and %s gives %q", spec.Name.Name, rest)
	}

	obj := pass.TypesInfo.Defs[spec.Name].(*types.TypeName)
	if !res.owned[obj] {
		res.owned[obj] = true
		res.Owned = append(res.Owned, obj)
	}

	return ""
}

// argIndex returns the call-argument index of the parameter of decl named name, the receiver
// first. A field without names, such as the int in func(int), is one argument.
func argIndex(decl *ast.FuncDecl, name string) (int, bool) {
	var fields []*ast.Field
	if decl.Recv != nil {
		fields = append(fields, decl.Recv.List...)
	}

	fields = append(fields, decl.Type.Params.List...)

	i := 0

	for _, field := range fields {
		if len(field.Names) == 0 {
			i++
			continue
		}

		for _, id := range field.Names {
			if id.Name == name && name != "_" {
				return i, true
			}

			i++
		}
	}

	return 0, false
}
