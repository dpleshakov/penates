// Package jsonc edits JSONC documents surgically: object members and array
// elements are read and edited by RFC 6901 JSON Pointer while the comments and
// formatting of untouched parts survive byte for byte. It is pure text
// manipulation and performs no I/O; the json contributions of the RFC (7.4)
// rely on it to place values into shared JSONC files.
package jsonc
