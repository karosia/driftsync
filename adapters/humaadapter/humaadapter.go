package humaadapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/pb33f/libopenapi"

	"github.com/karosia/driftsync/ir"
)

// Adapter extracts the contract from an already-constructed huma.API.
// It does NOT build the api — the api is injected and only read here
// (separation of concerns: construction lives in sampleapi.New).
type Adapter struct {
	api huma.API
}

// New returns a huma Adapter. Idiomatic: humaadapter.New(api).
func New(api huma.API) *Adapter {
	return &Adapter{api: api}
}

// Extract implements ir.Adapter: code -> OpenAPI 3.1 Document.
func (a *Adapter) Extract(_ context.Context) (*ir.Document, error) {
	// (1) Ask huma for the OpenAPI model it generated from the registered
	//     operations. huma defaults to 3.1, so this spec's version is "3.1.x".
	spec := a.api.OpenAPI()

	// (2) Serialize to YAML — our canonical form (reads well in a PR diff, and
	//     is exactly what method (1) writes to disk).
	raw, err := spec.YAML()
	if err != nil {
		return nil, fmt.Errorf("serialize huma spec: %w", err)
	}

	// (3) Parse with libopenapi. It models both 3.0 and 3.1 correctly;
	//     the model built here is what diff/canonicalizer will walk later.
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("load document: %w", err)
	}

	model, err := doc.BuildV3Model() // V3 = the 3.x-family model
	if err != nil {
		// Never swallow build errors: a silent loss here corrupts drift results.
		return nil, fmt.Errorf("build v3 model: %w", err)
	}

	// (4) Assert the 3.1 invariant. Under the current 3.x-only scope, fail loudly
	//     on anything else. (2.0 unsupported; 3.0 gets promoted to 3.1 later by
	//     the canonicalizer on the document side, never silently here.)
	version := model.Model.Version
	if !strings.HasPrefix(version, "3.1") {
		return nil, fmt.Errorf(
			"expected 3.1.x, got %q — the huma adapter must emit 3.1 only", version)
	}

	// (5) Wrap into the IR. From here on, the core knows nothing about huma.
	return &ir.Document{
		Version: version,
		Raw:     raw,
		Model:   &model.Model, // pointer to the v3high.Document value
	}, nil
}
