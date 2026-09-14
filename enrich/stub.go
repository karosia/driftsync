package enrich

import (
	"context"
	"fmt"
)

// StubEnricher is a deterministic, offline provider for tests and demos.
type StubEnricher struct{}

func (StubEnricher) Name() string { return "stub" }

func (StubEnricher) Describe(_ context.Context, req DescribeRequest) (string, error) {
	return fmt.Sprintf("The %s field of %s", req.Name, req.Schema), nil
}

// SuggestRename always confirms, deterministically, so tests/demos can
// exercise the hint path without a real provider.
func (StubEnricher) SuggestRename(_ context.Context, req RenameCandidate) (bool, string, error) {
	return true, fmt.Sprintf("stub: treating %q as %q renamed (same type %s)", req.FromName, req.ToName, req.TypeSig), nil
}
