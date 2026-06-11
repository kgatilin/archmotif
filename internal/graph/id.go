package graph

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Position is a source location used for ID construction. Line and Col are
// 1-indexed; an empty File means "no position" (used for Package nodes,
// foreign placeholders).
type Position struct {
	File string // forward-slash relative path, or empty
	Line int
	Col  int
}

// MakeID constructs a stable node ID per ADR-005:
//
//	<rel-path>:<line>:<col>:<kind>[:<name>][#<ordinal>]
//
// or, when File is empty (Package node, foreign placeholder),
//
//	pkg:<import-path>[:<kind>:<name>]
//
// The ordinal is 0 by default. Callers raise it to disambiguate nodes
// sharing the same path/line/col/kind/name (see Builder.Reserve).
func MakeID(kind NodeKind, pos Position, name string, ordinal int) string {
	var b strings.Builder
	if pos.File == "" {
		// Package or foreign placeholder. `name` carries the import path
		// for Package nodes; for other kinds we still include kind+name so
		// foreign Type/Function placeholders (which share the import path
		// with the Package) get distinct IDs.
		b.WriteString("pkg:")
		b.WriteString(name)
		if kind != NodePackage {
			b.WriteByte(':')
			b.WriteString(string(kind))
		}
		if ordinal > 0 {
			fmt.Fprintf(&b, "#%d", ordinal)
		}
		return b.String()
	}

	b.WriteString(filepath.ToSlash(pos.File))
	fmt.Fprintf(&b, ":%d:%d:%s", pos.Line, pos.Col, kind)
	if name != "" {
		b.WriteByte(':')
		b.WriteString(name)
	}
	if ordinal > 0 {
		fmt.Fprintf(&b, "#%d", ordinal)
	}
	return b.String()
}
