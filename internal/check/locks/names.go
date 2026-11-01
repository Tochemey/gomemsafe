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

package locks

import (
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// name words a lock key: its root, then each step of the path, a field by name and an element
// by the name of its index. A step the type cannot follow ends the name.
func (c *checker) name(u *universe, k key) string {
	var (
		name string
		t    types.Type
	)

	switch {
	case strings.HasPrefix(k.global, constPrefix):
		return strings.TrimPrefix(k.global, constPrefix)
	case k.global != "":
		name, t = c.globalName(k.global)
	default:
		name, t = c.valueName(k.root), k.root.Type()
	}

	for _, step := range k.steps() {
		if id, isIndex := strings.CutPrefix(step, "["); isIndex {
			id, _ = strings.CutSuffix(id, "]")
			n, _ := strconv.Atoi(id)
			name += "[" + c.name(u, u.indexKeys[n]) + "]"
			t = elementOf(t)

			continue
		}

		i, _ := strconv.Atoi(step)

		// The root of a captured or spilled variable is its address, one pointer further from
		// the struct than the source names.
		field, ok := fieldOf(t, i)
		for ptr, isPtr := t.Underlying().(*types.Pointer); !ok && isPtr; ptr, isPtr = t.Underlying().(*types.Pointer) {
			t = ptr.Elem()
			field, ok = fieldOf(t, i)
		}

		if !ok {
			break
		}

		name += "." + field.Name()
		t = field.Type()
	}

	return name
}

// elementOf returns the element type of a slice or an array, through any pointers to it: the
// root of a captured or spilled variable is its address, one pointer further than the source
// names.
func elementOf(t types.Type) types.Type {
	for ptr, isPtr := t.Underlying().(*types.Pointer); isPtr; ptr, isPtr = t.Underlying().(*types.Pointer) {
		t = ptr.Elem()
	}

	switch x := t.Underlying().(type) {
	case *types.Slice:
		return x.Elem()
	case *types.Array:
		return x.Elem()
	}

	return types.Typ[types.Invalid]
}

// globalName words a global by its key, a package path and a name, qualified by its package
// when it is another's, and returns its type if the package is imported. A package reached
// through facts alone is named by the last element of its path.
func (c *checker) globalName(global string) (string, types.Type) {
	dot := strings.LastIndex(global, ".")
	path, name := global[:dot], global[dot+1:]

	if path == c.pass.Pkg.Path() {
		if obj := c.pass.Pkg.Scope().Lookup(name); obj != nil {
			return name, obj.Type()
		}
	}

	for _, imported := range c.pass.Pkg.Imports() {
		if imported.Path() == path {
			if obj := imported.Scope().Lookup(name); obj != nil {
				return imported.Name() + "." + name, obj.Type()
			}
		}
	}

	return path[strings.LastIndex(path, "/")+1:] + "." + name, types.Typ[types.Invalid]
}

// valueName words the root of a key as the source names it, or as "..." when the source
// gives it no name, such as the element a range loop is at. Fields, elements, conversions,
// constants, globals, and loads never root a key: resolve keys them by what they are reached
// from; a receive is the only other unary operation whose result can hold a lock.
func (c *checker) valueName(v ssa.Value) string {
	switch x := v.(type) {
	case *ssa.Parameter, *ssa.FreeVar:
		return v.Name()
	case *ssa.UnOp:
		return "(" + x.Op.String() + c.valueName(x.X) + ")"
	case *ssa.Alloc:
		elem := x.Type().Underlying().(*types.Pointer).Elem()

		switch x.Comment {
		case "new":
			return "new(" + types.TypeString(elem, types.RelativeTo(c.pass.Pkg)) + ")"
		case "complit", "":
			return "&" + types.TypeString(elem, types.RelativeTo(c.pass.Pkg)) + "{...}"
		}

		return x.Comment
	case *ssa.Phi:
		if x.Comment != "" && !strings.HasPrefix(x.Comment, "range") {
			return x.Comment
		}

		return "..."
	case *ssa.Call:
		switch {
		case x.Call.IsInvoke():
			return c.valueName(x.Call.Value) + "." + x.Call.Method.Name() + "()"
		case x.Call.StaticCallee() != nil:
			return x.Call.StaticCallee().Name() + "()"
		}

		return c.valueName(x.Call.Value) + "()"
	case *ssa.Extract:
		return c.valueName(x.Tuple) + "[" + strconv.Itoa(x.Index) + "]"
	}

	return "..."
}
