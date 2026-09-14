package diff_test

import (
	"strings"
	"testing"

	"github.com/karosia/driftsync/adapters/specfile"
	"github.com/karosia/driftsync/canonicalize"
	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/ir"
)

// mustDoc writes yaml to a temp file, loads + canonicalizes it. Test helper.
func mustDoc(t *testing.T, yaml string) *ir.Document {
	t.Helper()
	f := t.TempDir() + "/spec.yaml"
	if err := writeFile(f, yaml); err != nil {
		t.Fatal(err)
	}
	doc, err := specfile.New(f).Extract(ctx())
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	c, err := canonicalize.Apply(doc)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	return c
}

// find returns the change matching kind+location substring, or nil.
func find(r *diff.Report, kind diff.ChangeKind, locContains string) *diff.Change {
	for i := range r.Changes {
		c := &r.Changes[i]
		if c.Kind == kind && contains(c.Location, locContains) {
			return c
		}
	}
	return nil
}

func TestDiff_PropertyRemoved_DirectionalSeverity(t *testing.T) {
	// User is used in BOTH a response (GET) and a request (POST body);
	// removing `email` must be BREAKING as response, info as request.
	published := mustDoc(t, specUserBothDirections(`
        id:    {type: string}
        email: {type: string}`))
	code := mustDoc(t, specUserBothDirections(`
        id: {type: string}`)) // email removed

	r := diff.Diff(published, code)

	resp := find(r, diff.PropertyRemoved, "User [response].email")
	if resp == nil || resp.Severity != diff.Breaking {
		t.Errorf("response email removal should be BREAKING, got %+v", resp)
	}
	req := find(r, diff.PropertyRemoved, "User [request].email")
	if req == nil || req.Severity != diff.Info {
		t.Errorf("request email removal should be info, got %+v", req)
	}
}

func TestDiff_OperationAdded(t *testing.T) {
	published := mustDoc(t, minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}`))
	code := mustDoc(t, minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}
  /users/{id}: {delete: {operationId: del, responses: {'204': {description: NC}}}}`))

	r := diff.Diff(published, code)
	if find(r, diff.OperationAdded, "DELETE /users/{id}") == nil {
		t.Error("expected DELETE /users/{id} operation_added")
	}
}

func TestDiff_ParameterRequiredChanged_Breaking(t *testing.T) {
	published := mustDoc(t, paramSpec("false"))
	code := mustDoc(t, paramSpec("true")) // limit became required
	r := diff.Diff(published, code)
	c := find(r, diff.ParameterRequiredChanged, "query:limit")
	if c == nil || c.Severity != diff.Breaking {
		t.Errorf("param becoming required should be BREAKING, got %+v", c)
	}
}

func TestDiff_ResponseAdded(t *testing.T) {
	published := mustDoc(t, respSpec(""))
	code := mustDoc(t, respSpec(`'429': {description: Too Many}`))
	r := diff.Diff(published, code)
	c := find(r, diff.ResponseAdded, "response 429")
	if c == nil || c.Severity != diff.Info {
		t.Errorf("added response code should be info, got %+v", c)
	}
}

func TestDiff_NoChange(t *testing.T) {
	s := minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}`)
	r := diff.Diff(mustDoc(t, s), mustDoc(t, s))
	if len(r.Changes) != 0 {
		t.Errorf("identical specs should report no drift, got %+v", r.Changes)
	}
}

func TestDiff_SchemaAddedAndRemoved(t *testing.T) {
	// Different shapes so this stays an unrelated remove+add, not a rename
	// (see TestDiff_SchemaRenamed for the identical-shape case).
	published := mustDoc(t, minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}`)+`
components:
  schemas:
    Legacy:
      type: object
      properties: {id: {type: string}}
`)
	code := mustDoc(t, minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}`)+`
components:
  schemas:
    Widget:
      type: object
      properties: {name: {type: string}, count: {type: integer}}
`)
	r := diff.Diff(published, code)
	if c := find(r, diff.SchemaRemoved, "Legacy"); c == nil || c.Severity != diff.Info {
		t.Errorf("expected info schema_removed Legacy, got %+v", c)
	}
	if c := find(r, diff.SchemaAdded, "Widget"); c == nil || c.Severity != diff.Info {
		t.Errorf("expected info schema_added Widget, got %+v", c)
	}
	if find(r, diff.SchemaRenamed, "") != nil {
		t.Error("unrelated schemas of different shapes must not be matched as a rename")
	}
}

// TestDiff_SchemaRenamed is the schema-level counterpart to the property
// rename fix in #2/#3: a whole component schema renamed (same shape, new
// name) must be reported as ONE rename, not an unrelated remove+add, because
// patch needs to know to rewrite every existing $ref to the old name too.
func TestDiff_SchemaRenamed(t *testing.T) {
	published := mustDoc(t, minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}`)+`
components:
  schemas:
    UserDTO:
      type: object
      required: [id]
      properties: {id: {type: string}, email: {type: string}}
`)
	code := mustDoc(t, minimalPaths(`
  /users: {get: {operationId: list, responses: {'200': {description: OK}}}}`)+`
components:
  schemas:
    User:
      type: object
      required: [id]
      properties: {id: {type: string}, email: {type: string}}
`)
	r := diff.Diff(published, code)
	c := find(r, diff.SchemaRenamed, "UserDTO -> User")
	if c == nil {
		t.Fatalf("expected schema_renamed UserDTO -> User, got %+v", r.Changes)
	}
	if c.From != "UserDTO" || c.To != "User" {
		t.Errorf("From/To = %q/%q, want UserDTO/User", c.From, c.To)
	}
	if find(r, diff.SchemaRemoved, "UserDTO") != nil || find(r, diff.SchemaAdded, "User") != nil {
		t.Error("a matched rename must not also appear as a separate remove+add")
	}
}

// TestDiff_PropertyRemoved_CarriesFromRequired is a regression for the removal
// side of #2: removing a REQUIRED property must carry that fact along (patch
// needs it to also drop the name from `required`), same as a rename does.
func TestDiff_PropertyRemoved_CarriesFromRequired(t *testing.T) {
	// specUserBothDirections doesn't parameterize `required`, so build the spec
	// directly: published has User.required: [id, email] and email gets removed
	// while still required.
	published := mustDoc(t, `openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:
  /users:
    post:
      operationId: create-user
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/User'}
      responses:
        '201': {description: Created}
components:
  schemas:
    User:
      type: object
      required: [id, email]
      properties:
        id: {type: string}
        email: {type: string}
`)
	code := mustDoc(t, `openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:
  /users:
    post:
      operationId: create-user
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/User'}
      responses:
        '201': {description: Created}
components:
  schemas:
    User:
      type: object
      required: [id]
      properties:
        id: {type: string}
`)
	r := diff.Diff(published, code)
	c := find(r, diff.PropertyRemoved, "User [request].email")
	if c == nil {
		t.Fatalf("expected property_removed User.email, got %+v", r.Changes)
	}
	if !c.FromRequired {
		t.Errorf("expected FromRequired=true for a removed required property, got %+v", c)
	}
}

func TestDiff_RequiredDangling(t *testing.T) {
	// `ghost` is in `required` but has no matching property, on the published
	// side only — code side is clean.
	published := mustDoc(t, specUserBothDirections(`
        id: {type: string}`)+"\n      required: [id, ghost]")
	code := mustDoc(t, specUserBothDirections(`
        id: {type: string}`)+"\n      required: [id]")

	r := diff.Diff(published, code)
	c := find(r, diff.RequiredDangling, "User [request].required[ghost]")
	if c == nil || c.Severity != diff.Info || c.Note != "published" {
		t.Errorf("expected an info required_dangling flag for published ghost, got %+v", c)
	}
	if find(r, diff.RequiredDangling, "User [request].required[id]") != nil {
		t.Error("id names a real property and must not be flagged dangling")
	}
}

// TestDiff_PropertySignature_CatchesFacetsBeyondType covers what used to be
// completely invisible: an enum value removed, a format changed, and an
// array's item type changed all used to signature identically to the
// unchanged case (just "type:string"/"type:array"). Same-name properties, so
// these land as PropertyTypeChanged.
func TestDiff_PropertySignature_CatchesFacetsBeyondType(t *testing.T) {
	cases := []struct {
		name         string
		published    string
		code         string
		wantDetected bool
	}{
		{"enum value removed", "status: {type: string, enum: [a, b, c]}", "status: {type: string, enum: [a, b]}", true},
		{"enum unchanged (order differs)", "status: {type: string, enum: [a, b]}", "status: {type: string, enum: [b, a]}", false},
		{"format changed", "at: {type: string, format: date}", "at: {type: string, format: date-time}", true},
		{"array item type changed", "tags: {type: array, items: {type: string}}", "tags: {type: array, items: {type: integer}}", true},
		{"array item type unchanged", "tags: {type: array, items: {type: string}}", "tags: {type: array, items: {type: string}}", false},
		{"nullable added", "note: {type: string}", "note: {type: string, nullable: true}", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			published := mustDoc(t, specUserBothDirections("\n        "+tc.published))
			code := mustDoc(t, specUserBothDirections("\n        "+tc.code))
			r := diff.Diff(published, code)
			// find the field name: it's whatever comes before the first ':'.
			field := tc.published[:strings.IndexByte(tc.published, ':')]
			got := find(r, diff.PropertyTypeChanged, "User [request]."+field) != nil
			if got != tc.wantDetected {
				t.Errorf("PropertyTypeChanged detected = %v, want %v (changes: %+v)", got, tc.wantDetected, r.Changes)
			}
		})
	}
}

// TestDiff_PropertySignature_OneOfVariantChanged: a oneOf swapped for a
// different variant must be caught even though the outer type ("object" or
// absent) didn't change.
func TestDiff_PropertySignature_OneOfVariantChanged(t *testing.T) {
	published := mustDoc(t, specUserBothDirections(`
        contact: {oneOf: [{type: string}, {type: integer}]}`))
	code := mustDoc(t, specUserBothDirections(`
        contact: {oneOf: [{type: string}, {type: boolean}]}`))
	r := diff.Diff(published, code)
	if find(r, diff.PropertyTypeChanged, "User [request].contact") == nil {
		t.Errorf("expected a oneOf variant swap to be detected, got %+v", r.Changes)
	}
}
