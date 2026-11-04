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

// Package keeper keeps and returns the memory of its parameters in the ways the summary must
// record, and declares a borrow, which it must honour over the body.
package keeper

import (
	"fmt"
	"io"
)

// Node is a linked node.
type Node struct {
	next *Node
	n    int
}

// Registry holds nodes.
type Registry struct {
	nodes []*Node
	byID  map[int]*Node
}

var last *Node

// Keep appends n to r.
func Keep(r *Registry, n *Node) { r.nodes = append(r.nodes, n) } // want Keep:`^retains\(0x2\)$`

// Add puts n into r's map.
func (r *Registry) Add(id int, n *Node) { r.byID[id] = n } // want Add:`^retains\(0x4\)$`

// Remember keeps n in a global.
func Remember(n *Node) { last = n } // want Remember:`^retains\(0x1\)$`

// Through keeps n through Keep.
func Through(r *Registry, n *Node) { Keep(r, n) } // want Through:`^retains\(0x2\)$`

// Head returns n itself.
func Head(n *Node) *Node { return n } // want Head:`^returns\(0x1\)$`

// Next returns memory reachable from n.
func Next(n *Node) *Node { return n.next } // want Next:`^returns\(0x1\)$`

// Count reads n and keeps nothing.
func Count(n *Node) int { return n.n }

// Wrap returns a new node holding n.
func Wrap(n *Node) *Node { return &Node{next: n} } // want Wrap:`^returns\(0x1\)$`

// Both keeps n and returns it.
func Both(r *Registry, n *Node) *Node { // want Both:`^retains\(0x2\) returns\(0x2\)$`
	Keep(r, n)
	return n
}

// Borrow declares that it keeps nothing of n, whatever its body does: callers trust the
// declaration, and MS502 checks the body.
//
//memsafe:borrows n
func Borrow(r *Registry, n *Node) { r.nodes = append(r.nodes, n) }

// Take consumes n, so it keeps it.
//
//memsafe:consumes n
func Take(n *Node) { _ = n } // want Take:`^consumes\(0x1\) retains\(0x1\)$`

// Started hands n to a goroutine.
func Started(n *Node) { go Count(n) } // want Started:`^retains\(0x1\)$`

// Printed passes n to a printer known to keep nothing; printing may synchronise.
func Printed(n *Node) { fmt.Println(n) } // want Printed:`^syncs$`

// Wrapped returns an error that may hold err.
func Wrapped(err error) error { return fmt.Errorf("node: %w", err) } // want Wrapped:`^returns\(0x1\) syncs$`

// Dynamic passes n to a function value, which may keep it.
func Dynamic(f func(*Node), n *Node) { f(n) } // want Dynamic:`^retains\(0x2\) syncs$`

// Written passes b to an io.Writer, which must not retain it, and may synchronise.
func Written(w io.Writer, b []byte) { _, _ = w.Write(b) } // want Written:`^syncs$`

// Ping and Pong keep n between them: the summary reaches a fixpoint over the cycle.
func Ping(n *Node) { Pong(n) } // want Ping:`^retains\(0x1\)$`

// Pong keeps n, or passes it back to Ping.
func Pong(n *Node) { // want Pong:`^retains\(0x1\)$`
	if n.n > 0 {
		Ping(n)
	} else {
		last = n
	}
}
