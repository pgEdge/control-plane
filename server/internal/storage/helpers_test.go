package storage_test

import "github.com/pgEdge/control-plane/server/internal/storage"

var _ storage.Value = (*TestValue)(nil)

// TestValue is a valid Value implementation that can be used to test
// operations.
type TestValue struct {
	storage.StoredValue
	SomeField string `json:"some_field"`
}
