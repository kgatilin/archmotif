// Package api declares a non-interface contract type. Stage 2 must
// recognise it as a `type:` contract and find producers that return
// the type by value or pointer.
package api

// Request is the public request shape. Declared as `type:` in
// `.archmotif.yaml`.
type Request struct {
	ID      string
	Payload []byte
}

// NewRequest is the canonical constructor; a Returns producer of the
// `Request` contract.
func NewRequest(id string, payload []byte) Request {
	return Request{ID: id, Payload: payload}
}
