// Package apply executes a plan: applies an item completely or not at all, writes
// atomically through temp files, updates the lock on success, and reconciles after an
// interruption.
package apply
