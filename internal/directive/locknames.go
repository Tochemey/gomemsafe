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
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// resolvedLock is a lock found through a path of fields.
type resolvedLock struct {
	lock *types.Var
	path []int
}

// resolveRef finds the lock name names for decl: in a parameter, the receiver first, in a
// result when allowed, or among the globals.
func resolveRef(pass *analysis.Pass, decl *ast.FuncDecl, name string, allowResult bool) (LockRef, *types.Var, string) {
	parts := strings.Split(name, ".")

	var params []*ast.Field
	if decl.Recv != nil {
		params = append(params, decl.Recv.List...)
	}

	params = append(params, decl.Type.Params.List...)

	if index, lock, ok := resolveInFields(pass, params, parts); ok {
		return LockRef{Kind: LockParam, Index: index, Path: lock.path}, lock.lock, ""
	}

	if allowResult && decl.Type.Results != nil {
		if index, lock, ok := resolveInFields(pass, decl.Type.Results.List, parts); ok {
			return LockRef{Kind: LockResult, Index: index, Path: lock.path}, lock.lock, ""
		}
	}

	var ref LockRef

	if lock, ok := resolveGlobalPath(pass, parts, &ref.Path, &ref.Global); ok {
		ref.Kind = LockGlobal
		return ref, lock, ""
	}

	return LockRef{}, nil, fmt.Sprintf("+checklocks:%s does not resolve to a parameter, a result, or a global of %s",
		name, decl.Name.Name)
}

// resolveInFields finds parts[0] among the named fields, counting each unnamed field as one
// position, and the rest of parts in its type.
func resolveInFields(pass *analysis.Pass, fields []*ast.Field, parts []string) (int, resolvedLock, bool) {
	index := 0

	for _, field := range fields {
		if len(field.Names) == 0 {
			index++
			continue
		}

		for _, id := range field.Names {
			if id.Name != parts[0] || id.Name == "_" {
				index++
				continue
			}

			// A named parameter or result always defines a variable.
			lock, ok := resolveFrom(pass.TypesInfo.Defs[id].(*types.Var), parts[1:])

			return index, lock, ok
		}
	}

	return 0, resolvedLock{}, false
}

// resolveFrom follows parts, a path of field names, from the variable v.
func resolveFrom(v *types.Var, parts []string) (resolvedLock, bool) {
	if len(parts) == 0 {
		return resolvedLock{lock: v}, true
	}

	st, ok := StructOf(v.Type())
	if !ok {
		return resolvedLock{}, false
	}

	var path []int

	lock, ok := resolveFieldPath(st, parts, &path)

	return resolvedLock{lock: lock, path: path}, ok
}

// resolveFieldPath follows parts, a path of field names, through st, appending the field
// indexes to path. A name a struct promotes from an embedded field is found there, with the
// embedded field on the path.
func resolveFieldPath(st *types.Struct, parts []string, path *[]int) (*types.Var, bool) {
	var field *types.Var

	for i, name := range parts {
		if i > 0 {
			var ok bool

			if st, ok = StructOf(field.Type()); !ok {
				return nil, false
			}
		}

		var ok bool

		if field, ok = findField(st, name, path); !ok {
			return nil, false
		}
	}

	return field, true
}

// findField finds the field named name in st, directly or promoted from an embedded struct,
// appending the indexes that reach it to path.
func findField(st *types.Struct, name string, path *[]int) (*types.Var, bool) {
	for i := range st.NumFields() {
		if f := st.Field(i); f.Name() == name {
			*path = append(*path, i)
			return f, true
		}
	}

	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}

		inner, ok := StructOf(f.Type())
		if !ok {
			continue
		}

		mark := len(*path)
		*path = append(*path, i)

		if found, ok := findField(inner, name, path); ok {
			return found, true
		}

		*path = (*path)[:mark]
	}

	return nil, false
}

// resolveGlobalPath follows parts from a global variable of the package, setting key to the
// global's key.
func resolveGlobalPath(pass *analysis.Pass, parts []string, path *[]int, key *string) (*types.Var, bool) {
	global, ok := pass.Pkg.Scope().Lookup(parts[0]).(*types.Var)
	if !ok {
		return nil, false
	}

	lock, ok := resolveFrom(global, parts[1:])
	if !ok {
		return nil, false
	}

	*path = lock.path
	*key = GlobalKey(global)

	return lock.lock, true
}

// GlobalKey returns the key of a global variable: its package path and name joined by a dot,
// which tells apart same-named globals of different packages.
func GlobalKey(v *types.Var) string { return v.Pkg().Path() + "." + v.Name() }

// StructOf returns the struct type t is, or points to.
func StructOf(t types.Type) (*types.Struct, bool) {
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		t = ptr.Elem()
	}

	st, ok := t.Underlying().(*types.Struct)

	return st, ok
}

// LockKind reports whether t is a lock, and whether it is an RWMutex, from its name as
// checklocks decides it: a type whose name ends in Mutex, through a pointer too, or
// sync.Locker.
func LockKind(t types.Type) (rw, ok bool) {
	s := t.String()

	switch {
	case strings.HasSuffix(s, "RWMutex"):
		return true, true
	case strings.HasSuffix(s, "Mutex"), strings.HasSuffix(s, "sync.Locker"):
		return false, true
	}

	return false, false
}
