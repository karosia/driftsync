package diff_test

import (
	"strings"
	"testing"

	"github.com/karosia/driftsync/diff"
)

// Regression tests for transitive direction discovery. Before, only a
// component $ref'd DIRECTLY as a request/response body got a direction; every
// other component was never diffed, so `check` reported "no drift" for real
// breaking changes in list endpoints and nested objects.

// specWith builds a spec from a `paths:` block and a `components.schemas:` block.
func specWith(paths, schemas string) string {
	return `openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:` + paths + `
components:
  schemas:` + schemas + "\n"
}

const listUsersPath = `
  /users:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                type: array
                items: {$ref: '#/components/schemas/User'}`

func userSchema(props string) string {
	return `
    User:
      type: object
      properties:` + props
}

func mustBreaking(t *testing.T, r *diff.Report, kind diff.ChangeKind, loc string) {
	t.Helper()
	c := find(r, kind, loc)
	if c == nil {
		t.Fatalf("expected %s at %q, got none; changes: %+v", kind, loc, r.Changes)
	}
	if c.Severity != diff.Breaking {
		t.Errorf("%s at %q should be BREAKING, got %s", kind, loc, c.Severity)
	}
}

func TestDirection_ArrayItemsResponse(t *testing.T) {
	pub := mustDoc(t, specWith(listUsersPath, userSchema(`
        id:    {type: string}
        email: {type: string}`)))
	code := mustDoc(t, specWith(listUsersPath, userSchema(`
        id: {type: string}`)))

	r := diff.Diff(pub, code)
	mustBreaking(t, r, diff.PropertyRemoved, "User [response].email")
	if c := find(r, diff.PropertyRemoved, "User [request]"); c != nil {
		t.Errorf("User is response-only; unexpected request-direction change %+v", c)
	}
}

func TestDirection_NestedComponentRef(t *testing.T) {
	paths := `
  /orders/{id}:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Order'}`
	order := `
    Order:
      type: object
      properties:
        id:   {type: string}
        addr: {$ref: '#/components/schemas/Address'}`
	pub := mustDoc(t, specWith(paths, order+`
    Address:
      type: object
      properties:
        zip:  {type: string}
        city: {type: string}`))
	code := mustDoc(t, specWith(paths, order+`
    Address:
      type: object
      properties:
        zip: {type: string}`))

	mustBreaking(t, diff.Diff(pub, code), diff.PropertyRemoved, "Address [response].city")
}

func TestDirection_AllOfInRequestBody(t *testing.T) {
	paths := `
  /pets:
    post:
      requestBody:
        content:
          application/json:
            schema:
              allOf:
                - {$ref: '#/components/schemas/Pet'}
                - type: object
                  properties: {tag: {type: string}}
      responses:
        '201': {description: Created}`
	pub := mustDoc(t, specWith(paths, `
    Pet:
      type: object
      properties:
        name: {type: string}`))
	code := mustDoc(t, specWith(paths, `
    Pet:
      type: object
      required: [name, owner]
      properties:
        name:  {type: string}
        owner: {type: string}`))

	r := diff.Diff(pub, code)
	// A newly required request field breaks old clients that don't send it.
	mustBreaking(t, r, diff.PropertyAdded, "Pet [request].owner")
	mustBreaking(t, r, diff.RequiredAdded, "Pet [request].name")
}

func TestDirection_CyclicSchemaTerminates(t *testing.T) {
	paths := `
  /tree:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Node'}`
	node := func(extra string) string {
		return `
    Node:
      type: object
      properties:
        children:
          type: array
          items: {$ref: '#/components/schemas/Node'}
        parent: {$ref: '#/components/schemas/Node'}` + extra
	}
	pub := mustDoc(t, specWith(paths, node(`
        label: {type: string}`)))
	code := mustDoc(t, specWith(paths, node("")))

	mustBreaking(t, diff.Diff(pub, code), diff.PropertyRemoved, "Node [response].label")
}

func TestDirection_DefaultResponseAndHeaders(t *testing.T) {
	paths := `
  /things:
    get:
      responses:
        '200':
          description: OK
          headers:
            X-Page:
              schema: {$ref: '#/components/schemas/Page'}
        default:
          description: error
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Problem'}`
	schemas := func(problemExtra, pageExtra string) string {
		return `
    Problem:
      type: object
      properties:
        title: {type: string}` + problemExtra + `
    Page:
      type: object
      properties:
        next: {type: string}` + pageExtra
	}
	pub := mustDoc(t, specWith(paths, schemas(`
        detail: {type: string}`, `
        prev: {type: string}`)))
	code := mustDoc(t, specWith(paths, schemas("", "")))

	r := diff.Diff(pub, code)
	mustBreaking(t, r, diff.PropertyRemoved, "Problem [response].detail")
	mustBreaking(t, r, diff.PropertyRemoved, "Page [response].prev")
}

func TestDirection_UnreferencedSchemaStillDiffed(t *testing.T) {
	paths := `
  /ping:
    get:
      responses:
        '204': {description: No Content}`
	pub := mustDoc(t, specWith(paths, userSchema(`
        id:    {type: string}
        email: {type: string}`)))
	code := mustDoc(t, specWith(paths, userSchema(`
        id: {type: string}`)))

	r := diff.Diff(pub, code)
	// Reachable from nothing we trace: diffed in both directions, worst case
	// wins, and each change says why.
	mustBreaking(t, r, diff.PropertyRemoved, "User [response].email")
	for _, c := range r.Changes {
		if !strings.Contains(c.Note, "unreferenced") {
			t.Errorf("change on unreferenced schema should carry a note: %+v", c)
		}
	}
}
