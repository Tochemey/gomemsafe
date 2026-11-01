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

package directive

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/catalog"
)

// The checklocks annotations, as gVisor's checklocks spells them: a line comment with a space
// after the slashes, unlike //memsafe: directives.
const (
	checkPrefix         = "// +check"
	annLocks            = "// +checklocks:"
	annLocksRead        = "// +checklocksread:"
	annLocksAcquire     = "// +checklocksacquire:"
	annLocksRelease     = "// +checklocksrelease:"
	annLocksExclude     = "// +checklocksexclude:"
	annLocksIgnore      = "// +checklocksignore"
	annLocksForce       = "// +checklocksforce"
	annLocksFail        = "// +checklocksfail"
	annLocksReadAny     = "// +checklocksreadany"
	annLocksAlias       = "// +checklocksalias:"
	annLocksAcquireRead = "// +checklocksacquireread:"
	annLocksReleaseRead = "// +checklocksreleaseread:"
	annLocksExcludeWrit = "// +checklocksexcludewrite:"
	annAtomic           = "// +checkatomic"
	annAtomicIgnore     = "// +checkatomicignore"
	annAlignedIgnore    = "// +checkalignedignore"
)

const (
	// LockParam is a lock in a call argument, the receiver first.
	LockParam LockRefKind = iota
	// LockResult is a lock in a result of the call.
	LockResult
	// LockGlobal is a global lock.
	LockGlobal
)

// pendingAnnotations are checklocks annotations gomemsafe recognises but does not check yet.
// Each is reported, so that a contract nobody checks never looks checked.
var pendingAnnotations = []string{
	annLocksReadAny, annLocksAlias, annLocksAcquireRead, annLocksReleaseRead, annLocksExcludeWrit,
	annAtomicIgnore, annAtomic, annAlignedIgnore,
}

// lockFacts holds what the checklocks annotations of one package declare.
type lockFacts struct {
	// Guarded lists the fields and globals with guards, in source order.
	Guarded []types.Object
	// Locks lists the locks some annotation names, in source order.
	Locks []types.Object
	// Contracted lists the functions with a lock contract, in source order.
	Contracted []*types.Func

	guards    map[types.Object][]Guard
	locks     map[types.Object]bool
	contracts map[*types.Func]*LockContract
	forced    map[lineKey]bool
}

// annotated is the declaration a checklocks annotation documents.
type annotated struct {
	// fields are the struct fields of one declaration, with the struct that declares them.
	fields []*types.Var
	owner  *types.Struct
	// vars are the globals of a var spec or block.
	vars []*types.Var
	fn   *ast.FuncDecl
	typ  *ast.TypeSpec
}

// GuardsOf returns the locks the annotations of obj, a field or a global of this package, name
// as guarding it.
func (r *Result) GuardsOf(obj types.Object) []Guard { return r.lock.guards[origin(obj)] }

// IsLock reports whether some annotation of this package names obj, a field or a global, as a
// lock.
func (r *Result) IsLock(obj types.Object) bool { return r.lock.locks[origin(obj)] }

// LockContractOf returns the contract the checklocks annotations of fn's doc comment declare.
func (r *Result) LockContractOf(fn *types.Func) (*LockContract, bool) {
	c, ok := r.lock.contracts[fn.Origin()]
	return c, ok
}

// Forced reports whether a +checklocksforce annotation is on the line of pos: the assertion
// there is taken as true.
func (r *Result) Forced(fset *token.FileSet, pos token.Pos) bool {
	p := fset.Position(pos)
	return r.lock.forced[lineKey{p.Filename, p.Line}]
}

// LockFacts returns the annotated objects, for the summary analyzer to export.
func (r *Result) LockFacts() (guarded, locks []types.Object, contracted []*types.Func) {
	return r.lock.Guarded, r.lock.Locks, r.lock.Contracted
}

// parseLocks records every checklocks annotation of file. A file without one costs one scan
// of its comments.
func parseLocks(pass *analysis.Pass, res *Result, file *ast.File, report bool) {
	if !slices.ContainsFunc(file.Comments, func(group *ast.CommentGroup) bool {
		return slices.ContainsFunc(group.List, func(c *ast.Comment) bool { return strings.HasPrefix(c.Text, checkPrefix) })
	}) {
		return
	}

	targets := lockTargets(pass, file)

	for _, group := range file.Comments {
		target, documented := targets[group]

		for _, c := range group.List {
			if !strings.HasPrefix(c.Text, checkPrefix) {
				continue
			}

			switch {
			case documented:
				parseLock(pass, res, c, target, report)
			default:
				parseLockLine(pass, res, c, report)
			}
		}
	}
}

// lockTargets maps each comment group that documents a field, a global, a function, or a type
// to that declaration. A var block's doc comment documents every variable in the block.
func lockTargets(pass *analysis.Pass, file *ast.File) map[*ast.CommentGroup]annotated {
	targets := make(map[*ast.CommentGroup]annotated)
	add := func(group *ast.CommentGroup, a annotated) {
		if group != nil {
			targets[group] = a
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		ss, ok := n.(*ast.StructType)
		if !ok {
			return true
		}

		// A struct type literal always has a struct type: the package type-checked.
		st := pass.TypesInfo.TypeOf(ss).Underlying().(*types.Struct)
		i := 0

		for _, f := range ss.Fields.List {
			// An embedded field has no name and takes one position; one declaration may name
			// several fields, which share its annotations.
			fields := make([]*types.Var, 0, len(f.Names))

			for range max(1, len(f.Names)) {
				fields = append(fields, st.Field(i))
				i++
			}

			add(f.Doc, annotated{fields: fields, owner: st})
			add(f.Comment, annotated{fields: fields, owner: st})
		}

		return true
	})

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			add(d.Doc, annotated{fn: d})
		case *ast.GenDecl:
			switch d.Tok { //nolint:exhaustive // only var and type declarations carry annotations
			case token.VAR:
				var all []*types.Var

				for _, spec := range d.Specs {
					vs := spec.(*ast.ValueSpec)
					vars := globalsOf(pass, vs)
					all = append(all, vars...)

					add(vs.Doc, annotated{vars: vars})
					add(vs.Comment, annotated{vars: vars})
				}

				add(d.Doc, annotated{vars: all})
			case token.TYPE:
				for _, spec := range d.Specs {
					ts := spec.(*ast.TypeSpec)
					add(ts.Doc, annotated{typ: ts})
					add(ts.Comment, annotated{typ: ts})
				}

				if !d.Lparen.IsValid() {
					add(d.Doc, annotated{typ: d.Specs[0].(*ast.TypeSpec)})
				}
			default:
			}
		}
	}

	return targets
}

// globalsOf returns the variables a var spec declares, the blank ones left out.
func globalsOf(pass *analysis.Pass, vs *ast.ValueSpec) []*types.Var {
	var vars []*types.Var

	for _, name := range vs.Names {
		if v, ok := pass.TypesInfo.Defs[name].(*types.Var); ok && name.Name != "_" {
			vars = append(vars, v)
		}
	}

	return vars
}

// parseLockLine records an annotation on a line of code: a force, an ignore, which suppresses
// MS403 on that line, or checklocks' own test marker, which the conformance harness reads.
// Anything else is malformed there.
func parseLockLine(pass *analysis.Pass, res *Result, c *ast.Comment, report bool) {
	p := pass.Fset.Position(c.Pos())
	key := lineKey{p.Filename, p.Line}

	for _, pending := range pendingAnnotations {
		if strings.HasPrefix(c.Text, pending) {
			malformedLock(pass, c.Pos(), report, "%s is not supported yet by this version of gomemsafe", annotationName(c.Text))
			return
		}
	}

	switch {
	case strings.HasPrefix(c.Text, annLocksForce):
		res.lock.forced[key] = true
	case strings.HasPrefix(c.Text, annLocksIgnore):
		res.allowed[key] = append(res.allowed[key], string(catalog.UnguardedField))
	case strings.HasPrefix(c.Text, annLocksFail):
	default:
		malformedLock(pass, c.Pos(), report, "%s must be in the doc comment of a field, a variable, or a function",
			annotationName(c.Text))
	}
}

// parseLock records the annotation c, which documents target. A malformed or unsupported
// annotation is reported at the declaration, where checklocks reports it too.
func parseLock(pass *analysis.Pass, res *Result, c *ast.Comment, target annotated, report bool) {
	var (
		msg string
		pos token.Pos
	)

	switch {
	case target.fields != nil:
		pos = target.fields[0].Pos()
	case target.vars != nil:
		pos = target.vars[0].Pos()
	case target.fn != nil:
		pos = target.fn.Pos()
	default:
		pos = target.typ.Pos()
	}

	// checklocks' own test marker says the declaration fails one of its checks; the
	// conformance run reads it, and the analyzer leaves it alone.
	if strings.HasPrefix(c.Text, annLocksFail) {
		return
	}

	for _, pending := range pendingAnnotations {
		if strings.HasPrefix(c.Text, pending) {
			malformedLock(pass, pos, report, "%s is not supported yet by this version of gomemsafe", annotationName(c.Text))
			return
		}
	}

	switch {
	case target.fields != nil:
		msg = res.lock.declareFieldGuard(pass, c.Text, target)
	case target.vars != nil:
		msg = res.lock.declareGlobalGuard(pass, c.Text, target.vars)
	case target.fn != nil:
		msg = res.lock.declareContract(pass, c.Text, target.fn)
	case strings.HasPrefix(c.Text, annLocksIgnore):
		// On a type, checklocks ignores the annotation; so does gomemsafe, for compatibility.
	default:
		msg = fmt.Sprintf("%s must be in the doc comment of a field, a variable, or a function", annotationName(c.Text))
	}

	if msg != "" {
		malformedLock(pass, pos, report, "%s", msg)
	}
}

// declareFieldGuard adds the guard of the struct fields of one declaration. The lock is
// resolved in the struct that declares them first, then among the package's globals, as
// checklocks does.
func (l *lockFacts) declareFieldGuard(pass *analysis.Pass, text string, target annotated) string {
	name, ok := strings.CutPrefix(text, annLocks)
	if !ok {
		return fieldMisuse(text, target.fields[0])
	}

	parts := strings.Split(name, ".")

	var guard Guard

	lock, ok := resolveFieldPath(target.owner, parts, &guard.Path)
	if !ok {
		if lock, ok = resolveGlobalPath(pass, parts, &guard.Path, &guard.Global); !ok {
			return fmt.Sprintf("+checklocks:%s does not resolve to a field of the struct declaring %s, or to a global",
				name, target.fields[0].Name())
		}
	}

	if _, isLock := LockKind(lock.Type()); !isLock {
		return fmt.Sprintf("+checklocks:%s names %s, which is not a Mutex, an RWMutex, or a sync.Locker", name, lock.Name())
	}

	guard.Name = name

	for _, field := range target.fields {
		if msg := l.addGuard(field, lock, guard); msg != "" {
			return msg
		}
	}

	return ""
}

// declareGlobalGuard adds the guard of the globals of one var spec or block. The lock is a
// global, or a field of one.
func (l *lockFacts) declareGlobalGuard(pass *analysis.Pass, text string, vars []*types.Var) string {
	name, ok := strings.CutPrefix(text, annLocks)
	if !ok {
		return fieldMisuse(text, vars[0])
	}

	var guard Guard

	lock, ok := resolveGlobalPath(pass, strings.Split(name, "."), &guard.Path, &guard.Global)
	if !ok {
		return fmt.Sprintf("+checklocks:%s does not resolve to a global lock, for %s", name, vars[0].Name())
	}

	if _, isLock := LockKind(lock.Type()); !isLock {
		return fmt.Sprintf("+checklocks:%s names %s, which is not a Mutex, an RWMutex, or a sync.Locker", name, lock.Name())
	}

	guard.Name = name

	for _, v := range vars {
		if msg := l.addGuard(v, lock, guard); msg != "" {
			return msg
		}
	}

	return ""
}

// addGuard records guard on obj, and lock as a lock.
func (l *lockFacts) addGuard(obj types.Object, lock *types.Var, guard Guard) string {
	for _, g := range l.guards[obj] {
		if g.Name == guard.Name {
			return fmt.Sprintf("+checklocks:%s is declared twice on %s", guard.Name, obj.Name())
		}
	}

	if _, seen := l.guards[obj]; !seen {
		l.Guarded = append(l.Guarded, obj)
	}

	l.guards[obj] = append(l.guards[obj], guard)
	l.addLock(lock)

	return ""
}

// addLock records that an annotation names lock.
func (l *lockFacts) addLock(lock *types.Var) {
	if !l.locks[lock] {
		l.locks[lock] = true
		l.Locks = append(l.Locks, lock)
	}
}

// declareContract adds one annotation of decl's doc comment to its lock contract.
func (l *lockFacts) declareContract(pass *analysis.Pass, text string, decl *ast.FuncDecl) string {
	fn := pass.TypesInfo.Defs[decl.Name].(*types.Func)

	c, seen := l.contracts[fn]
	if !seen {
		c = &LockContract{}
	}

	var msg string

	switch {
	case strings.HasPrefix(text, annLocksIgnore):
		c.Ignore = true
	case strings.HasPrefix(text, annLocks):
		msg = l.addRef(pass, c, decl, strings.TrimPrefix(text, annLocks), &c.Requires, true, false)
	case strings.HasPrefix(text, annLocksRead):
		msg = l.addRef(pass, c, decl, strings.TrimPrefix(text, annLocksRead), &c.Requires, false, false)
	case strings.HasPrefix(text, annLocksAcquire):
		msg = l.addRef(pass, c, decl, strings.TrimPrefix(text, annLocksAcquire), &c.Acquires, true, true)
	case strings.HasPrefix(text, annLocksRelease):
		msg = l.addRef(pass, c, decl, strings.TrimPrefix(text, annLocksRelease), &c.Releases, true, false)
	case strings.HasPrefix(text, annLocksExclude):
		msg = l.addRef(pass, c, decl, strings.TrimPrefix(text, annLocksExclude), &c.Excludes, false, false)
	default:
		return fmt.Sprintf("%s is not valid on a function; use +checklocks, +checklocksread, +checklocksacquire, "+
			"+checklocksrelease, +checklocksexclude, or +checklocksignore", annotationName(text))
	}

	if msg != "" {
		return msg
	}

	if !seen {
		l.contracts[fn] = c
		l.Contracted = append(l.Contracted, fn)
	}

	return ""
}

// addRef resolves name for decl and appends it to set. A lock is required or excluded, not
// both, and named once per contract. allowResult lets the lock be in a result, for acquires.
func (l *lockFacts) addRef(pass *analysis.Pass, c *LockContract, decl *ast.FuncDecl, name string, set *[]LockRef, exclusive, allowResult bool) string {
	excluded := set == &c.Excludes

	for _, declared := range []struct {
		refs     []LockRef
		excluded bool
	}{{c.Requires, false}, {c.Acquires, false}, {c.Releases, false}, {c.Excludes, true}} {
		for _, ref := range declared.refs {
			switch {
			case ref.Name != name:
			case excluded != declared.excluded:
				return fmt.Sprintf("%s is both required and excluded by %s", name, decl.Name.Name)
			default:
				return fmt.Sprintf("+checklocks:%s is declared twice on %s", name, decl.Name.Name)
			}
		}
	}

	ref, lock, msg := resolveRef(pass, decl, name, allowResult)
	if msg != "" {
		return msg
	}

	rw, isLock := LockKind(lock.Type())

	switch {
	case !isLock:
		return fmt.Sprintf("%s names %s, which is not a Mutex, an RWMutex, or a sync.Locker", name, lock.Name())
	case !exclusive && !excluded && !rw:
		return fmt.Sprintf("+checklocksread:%s names %s, which is not an RWMutex", name, lock.Name())
	}

	ref.Name, ref.Exclusive = name, exclusive
	*set = append(*set, ref)

	l.addLock(lock)

	return ""
}

// fieldMisuse words an annotation other than +checklocks on a field or a global.
func fieldMisuse(text string, obj types.Object) string {
	if strings.HasPrefix(text, annLocksIgnore) {
		// On a field, the annotation only silences checklocks' inference of guards, which
		// gomemsafe does not do; the annotation is accepted so that annotated code works unchanged.
		return ""
	}

	return fmt.Sprintf("%s is not valid on %s; a field or a variable takes +checklocks:<lock>", annotationName(text), obj.Name())
}

// annotationName returns the annotation of text without its argument: "+checklocks".
func annotationName(text string) string {
	name := strings.TrimPrefix(text, "// ")
	if i := strings.IndexAny(name, ": "); i >= 0 {
		name = name[:i]
	}

	return name
}

// origin returns the declared object of obj, which for a field of an instantiated generic type
// is the field of the generic one.
func origin(obj types.Object) types.Object {
	if v, ok := obj.(*types.Var); ok {
		return v.Origin()
	}

	return obj
}

func malformedLock(pass *analysis.Pass, pos token.Pos, report bool, format string, args ...any) {
	if report {
		pass.Report(analysis.Diagnostic{Pos: pos, Category: string(catalog.MalformedDirective),
			Message: string(catalog.MalformedDirective) + ": " + fmt.Sprintf(format, args...)})
	}
}
