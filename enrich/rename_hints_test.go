package enrich_test

import (
	"context"
	"testing"

	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/enrich"
)

// alwaysNoEnricher confirms nothing — used to prove SuggestRenames respects
// the provider's answer instead of always finding a hint.
type alwaysNoEnricher struct{}

func (alwaysNoEnricher) Name() string { return "always-no" }
func (alwaysNoEnricher) Describe(context.Context, enrich.DescribeRequest) (string, error) {
	return "", nil
}
func (alwaysNoEnricher) SuggestRename(context.Context, enrich.RenameCandidate) (bool, string, error) {
	return false, "", nil
}

func propRemoved(schema, prop, typeSig string) diff.Change {
	return diff.Change{Kind: diff.PropertyRemoved, TypeSig: typeSig,
		Target: diff.Target{Schema: schema, Direction: diff.Request, Property: prop}}
}
func propAdded(schema, prop, typeSig string) diff.Change {
	return diff.Change{Kind: diff.PropertyAdded, TypeSig: typeSig,
		Target: diff.Target{Schema: schema, Direction: diff.Request, Property: prop}}
}

func TestSuggestRenames_UnambiguousPairConfirmed(t *testing.T) {
	r := &diff.Report{Changes: []diff.Change{
		propRemoved("Product", "qty", "type:integer"),
		propAdded("Product", "quantity", "type:integer"),
	}}
	hints := enrich.SuggestRenames(context.Background(), enrich.StubEnricher{}, r)
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %+v", hints)
	}
	h := hints[0]
	if h.Schema != "Product" || h.From != "qty" || h.To != "quantity" {
		t.Errorf("unexpected hint: %+v", h)
	}
}

func TestSuggestRenames_AmbiguousPairSkipped(t *testing.T) {
	// Two removed candidates of the same type — genuinely ambiguous, must not
	// guess.
	r := &diff.Report{Changes: []diff.Change{
		propRemoved("Product", "qty", "type:integer"),
		propRemoved("Product", "amount", "type:integer"),
		propAdded("Product", "quantity", "type:integer"),
	}}
	hints := enrich.SuggestRenames(context.Background(), enrich.StubEnricher{}, r)
	if len(hints) != 0 {
		t.Errorf("ambiguous candidates must not produce a hint, got %+v", hints)
	}
}

func TestSuggestRenames_DifferentTypeNotPaired(t *testing.T) {
	r := &diff.Report{Changes: []diff.Change{
		propRemoved("Product", "qty", "type:integer"),
		propAdded("Product", "quantity", "type:string"), // different type
	}}
	hints := enrich.SuggestRenames(context.Background(), enrich.StubEnricher{}, r)
	if len(hints) != 0 {
		t.Errorf("a type mismatch must not be paired, got %+v", hints)
	}
}

func TestSuggestRenames_ProviderDeclines(t *testing.T) {
	r := &diff.Report{Changes: []diff.Change{
		propRemoved("Product", "qty", "type:integer"),
		propAdded("Product", "quantity", "type:integer"),
	}}
	hints := enrich.SuggestRenames(context.Background(), alwaysNoEnricher{}, r)
	if len(hints) != 0 {
		t.Errorf("expected no hints when the provider declines, got %+v", hints)
	}
}

func TestSuggestRenames_UnrelatedKindsIgnored(t *testing.T) {
	// PropertyRenamed already has a From/To — SuggestRenames must not touch it.
	r := &diff.Report{Changes: []diff.Change{
		{Kind: diff.PropertyRenamed, From: "a", To: "b", TypeSig: "type:string",
			Target: diff.Target{Schema: "User"}},
	}}
	hints := enrich.SuggestRenames(context.Background(), enrich.StubEnricher{}, r)
	if len(hints) != 0 {
		t.Errorf("expected no hints for non-remove/add kinds, got %+v", hints)
	}
}
