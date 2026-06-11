// Package store is the round-trip integration fixture for
// internal/verify. It realises the motif-001 skeleton: an interface
// (UserStore), a concrete struct (SQLUserStore) implementing it, and
// a Find method on the struct that satisfies the interface contract.
package store

// UserStore is the abstract role <Iface>.
type UserStore interface {
	Find(id string) string
}

// SQLUserStore is the concrete role <Impl>.
type SQLUserStore struct {
	dsn string
}

// Find is the role <Method>: it lives on SQLUserStore (receiver_role:
// Impl) and realises UserStore.Find (realises: {role: Iface, method:
// Method}).
func (s *SQLUserStore) Find(id string) string {
	return s.dsn + ":" + id
}
