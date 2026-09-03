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
