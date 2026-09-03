package diff_test

import (
	"context"
	"os"
	"strings"
)

// ---- small utilities referenced by diff_test.go ----

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func ctx() context.Context { return context.Background() }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// ---- YAML spec templates: each test fills in the variable part ----

// specUserBothDirections builds a spec where component `User` is referenced by
// BOTH a response (GET) and a request body (POST) — so schema drift is diffed in
// both directions. `props` is the indented properties block for User.
func specUserBothDirections(props string) string {
	return `openapi: 3.0.3
info: {title: User API, version: '1.0.0'}
paths:
  /users/{id}:
    get:
      operationId: get-user
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: '#/components/schemas/User'}
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
      properties:` + props + "\n"
}

// minimalPaths builds a spec with just a `paths:` block (the caller supplies it).
func minimalPaths(paths string) string {
	return `openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:` + paths + "\n"
}

// paramSpec builds GET /users with a `limit` query param whose `required` value
// is the argument ("true"/"false").
func paramSpec(required string) string {
	return `openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:
  /users:
    get:
      operationId: list-users
      parameters:
        - name: limit
          in: query
          required: ` + required + `
          schema: {type: integer}
      responses:
        '200': {description: OK}
`
}

// respSpec builds GET /users whose responses always include 200, plus whatever
// extra response entry the caller passes (e.g. "'429': {description: Too Many}").
func respSpec(extra string) string {
	s := `openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:
  /users:
    get:
      operationId: list-users
      responses:
        '200': {description: OK}
`
	if extra != "" {
		s += "        " + extra + "\n"
	}
	return s
}
