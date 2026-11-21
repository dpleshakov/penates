// Package view decides what the interfaces show: the state of every item,
// once, from plain values — the kit item, its lock record, the
// lock-vs-target reconciliation, and the planner's verdict. The TUI renders
// a state as a glyph and a color, the CLI as a state word; one decision and
// two renderings, so the words and the glyphs cannot drift apart. The
// package also holds the prose both clients speak — the lines a conflict or
// an exclusion renders to — and the verbs and path lines of the apply
// summary both lay out. It performs no I/O of its own: internal/target is
// imported for the snapshot values the summary lines read, never called.
package view
