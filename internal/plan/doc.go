// Package plan is the pure decision core: kits, lock, snapshot, and selection in; a
// plan out — actions per item with reasons, structured conflicts per item for
// the client to render. It implements the
// behavior rules of the product concept (docs/concept.md) — among them the
// three-state conflict model and the reconciliation of the lock with the target
// by recorded hashes — and performs no I/O.
package plan
