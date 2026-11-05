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

package ownership

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
)

// ContractID is the check ID of a method that breaks the contract of an interface method it
// implements.
const ContractID = catalog.BrokenInterfaceContract

// Diagnostic texts of MS504. A breach names the implementation's parameter, then the interface
// method.
const (
	msgBreaksContract = "%s: %s %s"
	msgContractHere   = "%s declares its contract here"
	helpContract      = "make %s do what %s declares, or change the contract"

	breachConsumed = "consumes %s, which %s does not consume"
	breachTaken    = "takes back %s, which %s does not consume"
	breachKept     = "keeps %s, which %s borrows"
	breachView     = "returns a view into %s, which %s does not declare"
	breachEnded    = "ends the views of %s, which %s does not declare"
)

// implementor is a type of this package that may implement an interface: through a pointer to
// it, whose method set holds all of its methods.
type implementor struct {
	name *types.TypeName
	ptr  *types.Pointer
}

// checkImplementations reports the methods of this package's types that break the contract of
// an interface method they implement, whether declared here or promoted from an embedded field.
// An interface method's callers rely on its contract alone, as a trait signature binds its
// impls: the implementation must not take more from them than it declares, so that a contract
// allows every method that does less. The receiver is not checked, as a contract cannot name it.
// A generic interface is implemented by no type as written, so only its calls are checked.
func checkImplementations(pass *analysis.Pass, sum *summary.Result) {
	methods := sum.InterfaceContracts()
	if len(methods) == 0 {
		return
	}

	impls := implementors(pass.Pkg)
	seen := make(map[[2]*types.Func]bool)

	for _, method := range methods {
		iface := method.Signature().Recv().Type().Underlying().(*types.Interface)

		for _, t := range impls {
			if !types.Implements(t.ptr, iface) {
				continue
			}

			// A type that implements the interface has the method.
			obj, _, _ := types.LookupFieldOrMethod(t.ptr, false, method.Pkg(), method.Name())
			impl := obj.(*types.Func).Origin()

			key := [2]*types.Func{method, impl}
			if seen[key] || impl == method {
				continue
			}

			seen[key] = true

			reportBreaches(pass, sum, method, impl, t)
		}
	}
}

// reportBreaches reports each parameter on which impl, a method of t's method set, breaks the
// contract of method. It reports at impl, or at t when impl is declared in another package. When
// impl is promoted from an embedded interface, its own contract is what it does, and it does
// nothing when it has none.
func reportBreaches(pass *analysis.Pass, sum *summary.Result, method, impl *types.Func, t implementor) {
	pos := impl.Pos()
	if impl.Pkg() != pass.Pkg {
		pos = t.name.Pos()
	}

	methodName, implName := qualifiedMethod(pass, method), qualifiedMethod(pass, impl)

	for arg := 1; arg <= impl.Signature().Params().Len(); arg++ {
		format := breachOf(sum, method, impl, arg)
		if format == "" {
			continue
		}

		report.Report(pass, analysis.Diagnostic{
			Pos:            pos,
			Category:       ContractID.String(),
			Message:        fmt.Sprintf(msgBreaksContract, ContractID, implName, fmt.Sprintf(format, paramAt(impl, arg).Name(), methodName)),
			Related:        []analysis.RelatedInformation{{Pos: method.Pos(), Message: fmt.Sprintf(msgContractHere, methodName)}},
			SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpContract, implName, methodName)}},
		})
	}
}

// breachOf returns how impl breaks the contract of method on argument arg, as a format taking
// the parameter and the method, or "" when it does not. A caller holds no more claim on an
// argument the method consumes, so the implementation may do anything to it.
func breachOf(sum *summary.Result, method, impl *types.Func, arg int) string {
	consumed := sum.Consumes(method).Has(arg)
	view, isView := sum.ViewOf(impl)

	switch {
	case !consumed && sum.Consumes(impl).Has(arg):
		return breachConsumed
	case !consumed && sum.Invalidates(impl).Has(arg):
		return breachTaken
	case !sum.Retains(method).Has(arg) && (sum.Retains(impl) | sum.Returns(impl)).Has(arg):
		return breachKept
	case !consumed && isView && view.Arg == arg && !declaresView(sum, method, arg):
		return breachView
	case !consumed && sum.EventsOn(impl, arg) != 0 && sum.EventsOn(method, arg) == 0:
		return breachEnded
	}

	return ""
}

// declaresView reports whether method's contract says that its first result is a view into
// argument arg.
func declaresView(sum *summary.Result, method *types.Func, arg int) bool {
	view, isView := sum.ViewOf(method)
	return isView && view.Arg == arg
}

// implementors returns the types of pkg, by name; a pointer to an interface type has no methods,
// so it implements nothing. A generic type stands for its
// instance over its own type parameters, whose methods are the ones it declares.
func implementors(pkg *types.Package) []implementor {
	var out []implementor

	for _, name := range pkg.Scope().Names() {
		tn, isType := pkg.Scope().Lookup(name).(*types.TypeName)
		if !isType || tn.IsAlias() {
			continue
		}

		out = append(out, implementor{name: tn, ptr: types.NewPointer(instanceOf(tn.Type().(*types.Named)))})
	}

	return out
}

// instanceOf returns named instantiated over its own type parameters, or named itself when it
// has none. Instantiating with as many arguments as parameters, unvalidated, cannot fail.
func instanceOf(named *types.Named) types.Type {
	params := named.TypeParams()
	if params.Len() == 0 {
		return named
	}

	args := make([]types.Type, params.Len())
	for i := range args {
		args[i] = params.At(i)
	}

	inst, _ := types.Instantiate(nil, named, args, false)

	return inst
}

// qualifiedMethod names fn as Type.Method, or pkg.Type.Method for another package's type.
func qualifiedMethod(pass *analysis.Pass, fn *types.Func) string {
	recv := types.Unalias(fn.Signature().Recv().Type())
	if ptr, isPointer := recv.(*types.Pointer); isPointer {
		recv = ptr.Elem()
	}

	return types.TypeString(recv, func(p *types.Package) string {
		if p == pass.Pkg {
			return ""
		}

		return p.Name()
	}) + "." + fn.Name()
}
