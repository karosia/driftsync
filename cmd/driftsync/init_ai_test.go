package main

import (
	"context"
	"errors"
	"testing"

	"github.com/karosia/driftsync/llm"
)

type fakeClient struct {
	out string
	err error
}

func (f fakeClient) Name() string { return "fake" }
func (f fakeClient) Complete(context.Context, llm.Request) (string, error) {
	return f.out, f.err
}

func TestCleanShellCommand(t *testing.T) {
	cases := []struct{ in, want string }{
		{"python genspec.py", "python genspec.py"},
		{"  python genspec.py  \n", "python genspec.py"},
		{"```bash\npython genspec.py\n```", "python genspec.py"},
		{"```\npython genspec.py\n```", "python genspec.py"},
		{"python genspec.py\nthis writes openapi.gen.yaml", "python genspec.py"}, // extra prose line dropped
		{"", ""},
	}
	for _, tc := range cases {
		if got := cleanShellCommand(tc.in); got != tc.want {
			t.Errorf("cleanShellCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDraftCodeCommand(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := fakeClient{out: "```bash\nmake openapi.gen.yaml\n```"}
		got, err := draftCodeCommand(context.Background(), client, "a go service using make")
		if err != nil {
			t.Fatal(err)
		}
		if got != "make openapi.gen.yaml" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("provider error propagates", func(t *testing.T) {
		client := fakeClient{err: errors.New("boom")}
		if _, err := draftCodeCommand(context.Background(), client, "whatever"); err == nil {
			t.Error("expected the provider error to propagate")
		}
	})

	t.Run("empty reply is an error", func(t *testing.T) {
		client := fakeClient{out: "   "}
		if _, err := draftCodeCommand(context.Background(), client, "whatever"); err == nil {
			t.Error("expected an error for an empty drafted command")
		}
	})
}

func TestBytesReplaceOnce(t *testing.T) {
	if got := bytesReplaceOnce([]byte("command: make openapi\n"), "command: make openapi", "command: foo"); string(got) != "command: foo\n" {
		t.Errorf("got %q", got)
	}
	if got := bytesReplaceOnce([]byte("nothing here"), "command: make openapi", "command: foo"); got != nil {
		t.Errorf("expected nil when the placeholder isn't found, got %q", got)
	}
}
