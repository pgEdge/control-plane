// Package migrate provides a mechanism for arbitrary migration operations that
// should block startup, such as moving Etcd objects from one key to another.
// IMPORTANT: migrations _must_ be written to be idempotent, and we should
// prefer non-destructive updates in order to allow rollbacks.
package migrate
