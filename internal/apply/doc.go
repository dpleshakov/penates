// Package apply executes a plan step by step: every step applies or removes one
// item completely or not at all, writes atomically through temp files, records
// the lock only for completed items, and reconciles after an interruption.
package apply
