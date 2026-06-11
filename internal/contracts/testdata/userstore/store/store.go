// Package store declares the contract interfaces. The fixture
// declares UserStore as a contract and AdminStore as an interface that
// embeds UserStore — Stage 2 must mark AdminStore transitively.
package store

// User is the entity returned by the contract methods.
type User struct {
	ID   string
	Name string
}

// UserStore is the contract interface. Two implementations live in
// this fixture (memStore and sqlStore in package store_impl).
type UserStore interface {
	Find(id string) (User, error)
	Save(u User) error
}

// AdminStore embeds UserStore. Per ADR-009 / issue #3, embedding a
// contract interface propagates the contract marker.
type AdminStore interface {
	UserStore
	Delete(id string) error
}

// MemStore is one implementation of UserStore.
type MemStore struct {
	rows map[string]User
}

// NewMemStore is a constructor that returns the contract type. Stage 2
// records this as a Returns producer of UserStore.
func NewMemStore() UserStore {
	return &MemStore{rows: make(map[string]User)}
}

// Find satisfies UserStore.
func (m *MemStore) Find(id string) (User, error) {
	u, ok := m.rows[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}

// Save satisfies UserStore.
func (m *MemStore) Save(u User) error {
	m.rows[u.ID] = u
	return nil
}

// SQLStore is a second implementation. Constructor returns the
// concrete type, not the interface — so it does *not* show up as a
// Returns producer of UserStore (only as an Implements producer via
// the type itself).
type SQLStore struct {
	dsn string
}

// NewSQLStore returns the concrete type. Not a Returns producer of
// UserStore — exists to verify the producer set is type-driven, not
// name-driven.
func NewSQLStore(dsn string) *SQLStore {
	return &SQLStore{dsn: dsn}
}

// Find satisfies UserStore.
func (s *SQLStore) Find(id string) (User, error) {
	return User{ID: id, Name: s.dsn}, nil
}

// Save satisfies UserStore.
func (s *SQLStore) Save(u User) error {
	_ = u
	return nil
}

// notFoundError is unexported; we expose it via a sentinel.
type notFoundError struct{}

func (notFoundError) Error() string { return "user: not found" }

var errNotFound = notFoundError{}
