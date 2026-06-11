// Package app exercises the contract producers from the consumer
// side. It contains a function that returns a UserStore via the
// MemStore constructor — this both confirms NewMemStore is callable
// and adds another use site for the interface.
package app

import (
	"userstore/api"
	"userstore/store"
)

// Bootstrap returns a UserStore. Counts as a second Returns producer
// of UserStore alongside store.NewMemStore.
func Bootstrap() store.UserStore {
	return store.NewMemStore()
}

// MakeRequest is a Returns producer of api.Request.
func MakeRequest() api.Request {
	return api.NewRequest("r1", []byte("hello"))
}
