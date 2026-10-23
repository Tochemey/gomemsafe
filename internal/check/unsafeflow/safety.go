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

package unsafeflow

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

// Diagnostic texts of MS104. The operation's description is the %s after the ID.
const (
	msgUnjustified    = "%s: %s without a // SAFETY: comment"
	helpUnjustified   = "say why the operation is safe in a // SAFETY: comment on the line above the statement"
	toUnsafePointer   = "conversion to unsafe.Pointer"
	fromUnsafePointer = "conversion from unsafe.Pointer"
	unsafeQualifier   = "unsafe."
)

// safetyUsage is the help text of the -unsafeflow.safety flag.
const safetyUsage = "report unsafe operations without a // SAFETY: comment (MS104)"

// safetyPrefix starts the comment line that justifies an unsafe operation.
const safetyPrefix = "SAFETY:"

// Names of the package unsafe builtins that MS104 covers besides Add.
const (
	sliceBuiltin      = "Slice"
	sliceDataBuiltin  = "SliceData"
	stringBuiltin     = "String"
	stringDataBuiltin = "StringData"
)

// enforceSafety is set by the -unsafeflow.safety flag.
var enforceSafety bool

// unsafeOperations are the package unsafe builtins that can break memory safety. Sizeof,
// Alignof, and Offsetof are left out: they only compute constants.
var unsafeOperations = map[string]bool{
	addBuiltin:        true,
	sliceBuiltin:      true,
	sliceDataBuiltin:  true,
	stringBuiltin:     true,
	stringDataBuiltin: true,
}

func init() {
	Analyzer.Flags.BoolVar(&enforceSafety, SafetyFlag, false, safetyUsage)
}

// reportUnjustified reports the first unsafe operation of each statement that no // SAFETY:
// comment justifies. The comment is a comment group that ends on the line above the statement
// and has a line starting with SAFETY: followed by the reason. A package-level variable
// declaration counts as a statement. Statements that start on the same line share that
// comment, so each line is reported once. Generated files are skipped: their justification
// belongs in the generator, such as cmd/cgo for _cgo_gotypes.go.
func reportUnjustified(pass *analysis.Pass, insp *inspector.Inspector) {
	justified := justifiedLines(pass)
	reported := make(map[token.Pos]bool)

	insp.WithStack([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		if !push {
			return true
		}

		op, ok := describeUnsafeOperation(pass.TypesInfo, n.(*ast.CallExpr))
		if !ok {
			return true
		}

		stmt := enclosingStatement(stack)
		file := pass.Fset.File(stmt.Pos())

		lines, checked := justified[file]
		if !checked {
			return true
		}

		line := rawLine(file, stmt.Pos())

		start := file.LineStart(line)
		if lines[line-1] || reported[start] {
			return true
		}

		reported[start] = true

		reportAt(pass, SafetyID, n.Pos(), fmt.Sprintf(msgUnjustified, SafetyID, op), helpUnjustified)

		return true
	})
}

// justifiedLines returns, for each file that is not generated, the lines on which a comment
// group holding a justification ends.
func justifiedLines(pass *analysis.Pass) map[*token.File]map[int]bool {
	lines := make(map[*token.File]map[int]bool, len(pass.Files))

	for _, f := range pass.Files {
		if ast.IsGenerated(f) {
			continue
		}

		file := pass.Fset.File(f.Pos())
		lines[file] = make(map[int]bool)

		for _, group := range f.Comments {
			if isJustification(group) {
				lines[file][rawLine(file, group.End())] = true
			}
		}
	}

	return lines
}

// rawLine returns the line of pos as it is in the file, ignoring //line directives, which can
// otherwise separate a comment from the statement under it.
func rawLine(file *token.File, pos token.Pos) int {
	return file.PositionFor(pos, false).Line
}

// isJustification reports whether group has a line starting with SAFETY: and a reason.
func isJustification(group *ast.CommentGroup) bool {
	for line := range strings.Lines(group.Text()) {
		reason, ok := strings.CutPrefix(strings.TrimSpace(line), safetyPrefix)
		if ok && strings.TrimSpace(reason) != "" {
			return true
		}
	}

	return false
}

// describeUnsafeOperation describes call if it is an unsafe operation: a conversion to or from
// unsafe.Pointer, or a call of one of unsafeOperations.
func describeUnsafeOperation(info *types.Info, call *ast.CallExpr) (string, bool) {
	if target, ok := convertedType(info, call); ok {
		switch {
		case isUnsafePointer(target):
			return toUnsafePointer, true
		case isUnsafePointer(info.TypeOf(call.Args[0])):
			return fromUnsafePointer, true
		}

		return "", false
	}

	name, ok := unsafeBuiltin(info, call.Fun)
	if !ok || !unsafeOperations[name] {
		return "", false
	}

	return unsafeQualifier + name, true
}

// enclosingStatement returns the innermost statement or variable declaration on stack. An
// expression always has one, so the loop stops before stack[0], the file.
func enclosingStatement(stack []ast.Node) ast.Node {
	i := len(stack) - 1
	for i > 0 && !isStatement(stack[i]) {
		i--
	}

	return stack[i]
}

func isStatement(n ast.Node) bool {
	switch n.(type) {
	case ast.Stmt, *ast.ValueSpec:
		return true
	}

	return false
}
