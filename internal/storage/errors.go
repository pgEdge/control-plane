package storage

import "errors"

// ErrNotFound indicates that no values were found for the given key.
var ErrNotFound = errors.New("key not found")

// ErrAlreadyExists indicates that a value could not be created because the key
// already exists.
var ErrAlreadyExists = errors.New("key already exists")

// ErrValueVersionMismatch indicates that the operation failed because the
// stored value's version didn't match the given value's version.
var ErrValueVersionMismatch = errors.New("value version mismatch")

// ErrOperationConstraintViolated indicates that one of the constraints on the
// operation, such as 'version = 0', was violated.
var ErrOperationConstraintViolated = errors.New("operation constraint violated")

// ErrDuplicateKeysInTransaction indicates that the transaction contained
// duplicate keys.
var ErrDuplicateKeysInTransaction = errors.New("duplicate keys in transaction")
