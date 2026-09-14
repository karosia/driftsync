package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/karosia/driftsync/llm"
)

// draftCodeCommand asks an LLM to write the code.command for a driftsync.yaml,
// given a prose description of the project's stack (`init --describe`). It's a
// one-time setup convenience only — nothing at detection/sync time depends on
// it or on any LLM at all, and the result is meant to be reviewed, not trusted
// blindly (cmdInit prints a caveat alongside it).
func draftCodeCommand(ctx context.Context, client llm.Client, description string) (string, error) {
	out, err := client.Complete(ctx, llm.Request{
		MaxTokens: 150,
		System: "You write a single shell command that extracts or generates an " +
			"OpenAPI 3.x spec from a project, given a prose description of its tech " +
			"stack and how the spec is produced. Reply with ONLY the shell command " +
			"on one line — no explanation, no markdown code fences, no surrounding " +
			"quotes. The command must write its output to the file " +
			"'openapi.gen.yaml' in the current directory.",
		Prompt: description,
	})
	if err != nil {
		return "", err
	}
	cmd := cleanShellCommand(out)
	if cmd == "" {
		return "", fmt.Errorf("model returned an empty command")
	}
	return cmd, nil
}

// cleanShellCommand strips markdown fences a model added despite instructions
// not to, and takes only the first line — a multi-line reply isn't one shell
// command that can drop into a single YAML scalar.
func cleanShellCommand(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```bash", "```sh", "```"} {
		s = strings.TrimPrefix(s, fence)
	}
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
