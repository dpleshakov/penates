// Package kit reads kit directories — kit.yaml, items, item.yaml, and contribution
// declarations — and validates everything the RFC requires of kit data: id syntax,
// version, source and target path rules, and shared-file targets. Validation errors
// carry file positions.
package kit
