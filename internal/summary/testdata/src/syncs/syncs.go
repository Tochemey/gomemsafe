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

// Package syncs has functions that synchronise with other goroutines, directly or through a
// callee, and look-alikes that do not.
package syncs

import "sync"

// Group mirrors golang.org/x/sync/errgroup.Group: its Wait synchronises through its WaitGroup.
type Group struct{ wg sync.WaitGroup }

// Waiter is an interface whose implementations the summary cannot see.
type Waiter interface{ Wait() }

type server struct{ mu sync.Mutex }

// Wait waits for every goroutine of the group.
func (g *Group) Wait() { g.wg.Wait() } // want Wait:`^syncs$`

// Receive receives from ch.
func Receive(ch chan int) int { return <-ch } // want Receive:`^syncs$`

// Send sends on ch.
func Send(ch chan int) { ch <- 1 } // want Send:`^syncs$`

// Join synchronises only through a callee in its own package, found by the fixpoint.
func Join(g *Group) { waitFor(g) } // want Join:`^syncs$`

// Count reads an atomic counter.
func Count(c *Counter) int64 { return c.n.Load() } // want Count:`^syncs$`

// Generic synchronises in its generic body.
func Generic[T any](ch chan T) T { return <-ch } // want Generic:`^syncs$`

// New returns a server, whose exported methods other packages can call.
func New() *server { return new(server) }

// Lock locks the server: a method of an unexported type, but with an exported name.
func (s *server) Lock() { s.mu.Lock() } // want Lock:`^syncs$`

// Unlock releases the server's lock, which completes no edge in its caller.
func (s *server) Unlock() { s.mu.Unlock() }

// Through waits through an interface, which may synchronise.
func Through(w Waiter) { w.Wait() } // want Through:`^syncs$`

// Calls calls a function value, which may synchronise.
func Calls(f func()) { f() } // want Calls:`^syncs$`

// Plain does nothing with other goroutines.
func Plain(x int) int { return x + 1 }

// Done signals a WaitGroup, a release that completes no edge in its caller.
func Done(wg *sync.WaitGroup) { wg.Done() }

// waitFor synchronises, but only its own package can call it, so it exports no fact.
func waitFor(g *Group) { g.Wait() }
