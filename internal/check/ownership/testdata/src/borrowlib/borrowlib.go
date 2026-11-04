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

// Package borrowlib keeps, returns, borrows, and consumes its arguments, for the packages that
// borrow values and pass them on, which learn of each through the summary analyzer's facts.
package borrowlib

// Node is a linked node.
type Node struct {
	Next *Node
	n    int
}

// Sink is an interface whose implementations may keep anything.
type Sink interface{ Put(n *Node) }

var kept []*Node

// Keep keeps n.
func Keep(n *Node) { kept = append(kept, n) }

// Head returns memory reachable from n.
func Head(n *Node) *Node { return n.Next }

// Inspect keeps nothing of n.
func Inspect(n *Node) int { return n.n }

// Lend keeps nothing of n, by contract.
//
//memsafe:borrows n
func Lend(n *Node) { _ = n }

// Take takes ownership of n.
//
//memsafe:consumes n
func Take(n *Node) { _ = n }
