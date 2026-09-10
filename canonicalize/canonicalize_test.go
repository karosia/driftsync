package canonicalize_test

import (
	"strings"
	"testing"

	"github.com/karosia/driftsync/canonicalize"
	"github.com/karosia/driftsync/ir"
)

func canon(t *testing.T, raw string) string {
	t.Helper()
	out, err := canonicalize.Apply(&ir.Document{Raw: []byte(raw)})
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	return string(out.Raw)
}

func TestCanonicalize_Nullable(t *testing.T) {
	got := canon(t, `
openapi: 3.0.3
info: {title: T, version: '1'}
paths: {}
components:
  schemas:
    X: {type: object, properties: {note: {type: string, nullable: true}}}`)
	if strings.Contains(got, "nullable") {
		t.Error("nullable keyword should be removed in 3.1")
	}
	if !strings.Contains(got, "null") {
		t.Error("type list should include null")
	}
}

func TestCanonicalize_ExclusiveMinimum(t *testing.T) {
	got := canon(t, `
openapi: 3.0.3
info: {title: T, version: '1'}
paths: {}
components:
  schemas:
    X: {type: object, properties: {n: {type: number, minimum: 0, exclusiveMinimum: true}}}`)
	if strings.Contains(got, "exclusiveMinimum: true") {
		t.Error("boolean exclusiveMinimum should become numeric")
	}
	if !strings.Contains(got, "exclusiveMinimum: 0") {
		t.Error("expected exclusiveMinimum: 0")
	}
}

func TestCanonicalize_VersionPromoted(t *testing.T) {
	got := canon(t, `
openapi: 3.0.3
info: {title: T, version: '1'}
paths: {}`)
	if !strings.Contains(got, "3.1.0") {
		t.Error("version should be promoted to 3.1.0")
	}
}
