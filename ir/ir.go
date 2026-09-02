package ir

import (
	"context"

	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Document is our canonical intermediate representation (IR).
// Everything downstream — diff, canonicalizer, agents — consumes ONLY this.
// Invariant: no matter which source an adapter reads, its output is OpenAPI 3.1.
type Document struct {
	Version string           // e.g. "3.1.0"; used to assert the 3.1 guarantee
	YAML    []byte           // canonical serialized form (for file emit / replay / PR diffs)
	Model   *v3high.Document // parsed model for structural traversal (walking paths, schemas)
}

// Adapter turns "the contract a source exposes" into a Document.
// One implementation per source: a code framework, or a published spec file.
// The core depends on this interface, never on a concrete adapter.
type Adapter interface {
	Extract(ctx context.Context) (*Document, error)
}
