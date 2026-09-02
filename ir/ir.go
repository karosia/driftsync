// ir/ir.go
package ir

import (
	"context"

	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Document is our canonical intermediate representation (IR).
// Everything downstream — canonicalizer, diff, agents — consumes ONLY this.
// Note: an adapter's output is OpenAPI 3.x; the canonicalizer normalizes both
// sides to 3.1 in the core, just before diff (never inside an adapter).
type Document struct {
	Version string           // exact source version, e.g. "3.0.3" or "3.1.0" (not yet canonicalized)
	Raw     []byte           // the source's serialized bytes as-is (YAML or JSON, as produced/published)
	Model   *v3high.Document // parsed model for structural traversal (walking paths, schemas)
}

// Adapter turns "the contract a source exposes" into a Document.
// One implementation per source: a code framework, or a published spec file.
type Adapter interface {
	Extract(ctx context.Context) (*Document, error)
}
