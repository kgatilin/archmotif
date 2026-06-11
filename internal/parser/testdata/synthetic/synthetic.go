// Package synthetic exercises every Stage 1 node and edge kind exactly
// once so the parser tests can assert their presence by walking the
// emitted graph deterministically.
//
// Layout:
//   - one struct (`Server`) and one interface (`Greeter`) — covers
//     Type{struct,interface}, Field, Method, Implements, Embeds.
//   - one stand-alone Function (`Run`) wraps every control-flow
//     primitive: Loop, Branch, Goroutine, Defer, ChannelOp (send/recv/
//     close), SyncPrim. Calls `Hello` so we get Calls and CallsFrom,
//     references `Hello` as a value, and constructs `Server` so we get
//     References and UsesType.
//   - DependsOn is exercised via the imports.
//   - Returns is exercised by `Run` returning `Greeter`.
package synthetic

import (
	"fmt"
	"sync"
)

// Greeter is implemented by Server.
type Greeter interface {
	Greet(name string) string
}

// Inner is embedded into Server below to exercise Embeds.
type Inner struct {
	Tag string
}

// Server is a typical struct with a field and an embedded type. It
// implements Greeter via the Greet method.
type Server struct {
	Inner
	Counter int
}

// Greet satisfies Greeter.
func (s *Server) Greet(name string) string {
	return fmt.Sprintf("hi %s from %s", name, s.Tag)
}

// Hello is a plain function called by Run; covers the Function node
// kind and the receiving end of a Calls edge.
func Hello() string {
	return "hello"
}

// Run wraps every control-flow primitive in one body. Returns a
// Greeter so we get a Returns edge.
func Run() Greeter {
	var mu sync.Mutex
	mu.Lock()         // SyncPrim
	defer mu.Unlock() // Defer + SyncPrim

	ch := make(chan int, 1)
	ch <- 1   // ChannelOp send
	v := <-ch // ChannelOp recv
	close(ch) // ChannelOp close
	_ = v
	fn := Hello
	_ = fn
	go Hello() // Goroutine

	for i := 0; i < 1; i++ { // Loop
		if i == 0 { // Branch
			_ = Hello() // Calls (and CallsFrom because inside Branch)
		}
	}
	return &Server{Inner: Inner{Tag: "t"}}
}
