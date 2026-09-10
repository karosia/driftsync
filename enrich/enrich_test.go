package enrich_test

import (
	"context"
	"testing"

	"github.com/karosia/driftsync/enrich"
	"github.com/karosia/driftsync/patch"

	yaml "gopkg.in/yaml.v3"
)

func strNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func TestEnrich_OnlyAddsGetDescriptions(t *testing.T) {
	patches := []patch.Patch{
		{Op: patch.OpAdd, Path: "/components/schemas/User/properties/createdAt", Value: strNode("x")},
		{Op: patch.OpRemove, Path: "/components/schemas/User/properties/legacy"},
		{Op: patch.OpAdd, Path: "/components/schemas/User/required/-", Value: strNode("email")}, // not a property add
	}
	res := enrich.Apply(context.Background(), enrich.StubEnricher{}, patches)

	if res.Enriched != 1 {
		t.Errorf("only the property add should be enriched, got %d", res.Enriched)
	}
	if patches[0].Description == "" {
		t.Error("property add should have a description")
	}
	if patches[1].Description != "" {
		t.Error("remove must not be described")
	}
	if patches[2].Description != "" {
		t.Error("required-array add must not be described")
	}
}
