// Package plan is the pure decision core: kits, lock, snapshot, and selection in; a
// plan out — actions per item with reasons, errors per item. It implements the
// three-state conflict model of the RFC, reconciles the lock with the target by
// recorded hashes (RFC 7.7), and performs no I/O.
package plan
