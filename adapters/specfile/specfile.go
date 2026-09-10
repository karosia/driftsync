package specfile

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pb33f/libopenapi"

	"github.com/karosia/driftsync/ir"
)

// Adapter loads a *published* OpenAPI document from a file on disk and exposes it
// through the same ir.Adapter contract as the code side. This is the document
// half of the symmetric "file -> IR" path.
type Adapter struct {
	path string
}

// New returns a specfile Adapter bound to a path. The file is not read until
// Extract is called (IO is deferred, mirroring humaadapter's construction split).
func New(path string) *Adapter {
	return &Adapter{path: path}
}

// Extract implements ir.Adapter: a published file -> an OpenAPI 3.x Document.
//
// KEY DIFFERENCE from the huma adapter: that one asserts 3.1 (huma only ever
// emits 3.1). This one accepts BOTH 3.0 and 3.1, because most specs published
// in the wild are still 3.0. It does NOT promote 3.0 -> 3.1 here; that
// normalization is the canonicalizer's job in the core, applied symmetrically
// to both sides right before diff. Version 2.0 is rejected explicitly
// (out of scope for now, on the roadmap).
func (a *Adapter) Extract(_ context.Context) (*ir.Document, error) {
	// (1) Read the published bytes as-is. We keep them verbatim in the IR so a
	//     later step can produce a MINIMAL diff against exactly what was published
	//     — re-rendering would reorder keys and inject diff noise.
	raw, err := os.ReadFile(a.path)
	if err != nil {
		return nil, fmt.Errorf("read spec file %q: %w", a.path, err)
	}

	// (2) Parse. NewDocument detects the spec version (2.0 / 3.0 / 3.1 / 3.2)
	//     but does not yet build a typed model.
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("parse spec file %q: %w", a.path, err)
	}

	// (3) Version gate. GetVersion returns the exact version string, e.g.
	//     "2.0", "3.0.3", "3.1.0". Accept the 3.x family; reject anything else
	//     with a clear, actionable message (never fail silently).
	version := doc.GetVersion()
	if !strings.HasPrefix(version, "3.") {
		return nil, fmt.Errorf(
			"unsupported OpenAPI version %q in %q — only 3.x is supported for now "+
				"(2.0 is on the roadmap)", version, a.path)
	}

	// (4) Build the v3 model. BuildV3Model covers the whole 3.x family into the
	//     same high-level model the core walks. Returns a single error.
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("build v3 model from %q: %w", a.path, err)
	}

	// (5) Wrap into the IR. Version may be "3.0.x" here — intentional and correct
	//     at the adapter boundary; the canonicalizer promotes to 3.1 downstream.
	return &ir.Document{
		Version: version,
		Raw:     raw,
		Model:   &model.Model,
	}, nil
}
