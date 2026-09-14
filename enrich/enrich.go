package enrich

import (
	"context"

	yaml "gopkg.in/yaml.v3"

	"github.com/karosia/driftsync/patch"
)

// DescribeRequest is everything a provider needs to write one description.
// It is provider-agnostic: no vendor types leak in here.
type DescribeRequest struct {
	Kind    string // "property" or "operation"
	Schema  string // owning schema name, e.g. "User"
	Name    string // property/field name, e.g. "createdAt"
	Snippet string // the added fragment (YAML) for type context
}

// RenameCandidate is a PropertyRemoved and a PropertyAdded change in the same
// schema+direction that share a type but weren't matched by diff's
// deterministic name-based rename detection (see matchRenames) — the
// "semantic rename" gap called out in the README (qty -> quantity).
type RenameCandidate struct {
	Schema, Direction string
	FromName, ToName  string
	TypeSig           string
}

// Enricher writes a one-line, human-readable description for a new field or
// endpoint, and can be asked to judge a RenameCandidate. One implementation per
// LLM provider (Anthropic, OpenAI, Gemini...). A returned error is non-fatal to
// the pipeline — the caller degrades.
type Enricher interface {
	Describe(ctx context.Context, req DescribeRequest) (string, error)
	// SuggestRename judges whether req is the same field renamed. It is
	// ADVISORY ONLY: the answer never changes what patch gets applied (still
	// a plain remove+add) — it only surfaces a note for the human reviewer.
	// ok=false (with any error) means "no hint", not "confirmed not a rename".
	SuggestRename(ctx context.Context, req RenameCandidate) (ok bool, note string, err error)
	Name() string // provider label, for logging/audit
}

// Result reports what happened, so failures are visible, never silent.
type Result struct {
	Enriched int
	Skipped  int // patches that don't take a description (remove/replace/required)
	Failed   int // provider errored on these; description left empty
}

// Apply fills the Description slot on ADD patches only, in place. Everything the
// deterministic stage decided (op/path/value) is untouched — the LLM only adds
// prose. A provider failure leaves that one description empty and is counted,
// not propagated: partial success is the contract.
func Apply(ctx context.Context, e Enricher, patches []patch.Patch) Result {
	var res Result
	for i := range patches {
		p := &patches[i]
		req, ok := describeRequestFor(*p)
		if !ok {
			res.Skipped++ // not an add; nothing to describe
			continue
		}
		desc, err := e.Describe(ctx, req)
		if err != nil {
			res.Failed++ // leave Description empty; the patch still applies structurally
			continue
		}
		p.Description = desc
		res.Enriched++
	}
	return res
}

// describeRequestFor returns a request only for add patches that target a
// property or (later) an operation. Others get no description.
func describeRequestFor(p patch.Patch) (DescribeRequest, bool) {
	if p.Op != patch.OpAdd {
		return DescribeRequest{}, false
	}
	schema, name, kind, ok := parseAddPath(p.Path)
	if !ok {
		return DescribeRequest{}, false
	}
	return DescribeRequest{
		Kind:    kind,
		Schema:  schema,
		Name:    name,
		Snippet: nodeToYAML(p.Value),
	}, true
}

// parseAddPath extracts (schema, name, kind) from a property-add pointer like
// "/components/schemas/User/properties/createdAt". required-array appends
// ("/.../required/-") and other shapes return ok=false (nothing to describe).
func parseAddPath(ptr string) (schema, name, kind string, ok bool) {
	toks := splitPointer(ptr)
	if len(toks) == 5 &&
		toks[0] == "components" && toks[1] == "schemas" && toks[3] == "properties" {
		return toks[2], toks[4], "property", true
	}
	return "", "", "", false
}

func nodeToYAML(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	out, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return string(out)
}
