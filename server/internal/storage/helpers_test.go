package storage_test

import "github.com/pgEdge/control-plane/server/internal/storage"

var _ storage.Value = (*TestValue)(nil)

// TestValue is a valid Value implementation that can be used to test
// operations.
type TestValue struct {
	version   int64  `json:"-"`
	SomeField string `json:"some_field"`
}

func (v *TestValue) Version() int64 {
	return v.version
}

func (v *TestValue) SetVersion(version int64) {
	v.version = version
}
