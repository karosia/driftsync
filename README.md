# driftsync

**Detect when your published API docs have drifted from your code — then fix
them, safely, behind a pull request.**

`driftsync` compares the OpenAPI spec your code actually produces against the
OpenAPI spec you've published, tells you exactly what diverged (and how badly),
proposes the edits to realign them, and opens a PR for a human to approve. It's
built to run automatically on every push via GitHub Actions.

Repo: https://github.com/karosia/driftsync

---

## The problem

Your API docs are correct the day you publish them. Then the code keeps moving —
a field gets renamed, a response gains a property, an endpoint appears — and the
published spec doesn't. Over time, "what the docs say" and "what the API does"
drift apart. Consumers integrate against a lie.

Most tools that fix this lean on an LLM to *read* your code and docs and guess
what's out of sync. driftsync takes the opposite stance:

> **Detection is deterministic. The LLM only ever writes prose, never decides
> what changed.** Your published contract is never overwritten automatically —
> every fix goes through a pull request you approve.

That's the whole design philosophy. Everything below follows from it.

---

## How it works

```
[your code] ──(extract)──► code spec ──┐
                                        ├─► canonicalize ─► diff ─► patch ─► apply ─► PR
[docs/openapi.yaml] ───────────────────┘        │            │        │        │
                                          normalize 3.0/3.1  detect  propose  edit the
                                          & fold dialect     drift   JSON      published
                                          differences                Patch     file
```

1. **Extract** — get an OpenAPI spec out of your code (framework-specific; the
   only part that depends on your stack).
2. **Canonicalize** — normalize both specs to OpenAPI 3.1 and fold
   equivalent-but-differently-written constructs, so dialect differences don't
   look like drift.
3. **Diff** — deterministically compare the two. Severity is **direction-aware**:
   the same change is breaking in a response but harmless in a request, and vice
   versa.
4. **Patch** — turn each drift into a concrete JSON Pointer (RFC 6901) edit,
   with the correct value copied from the code.
5. **Enrich** *(optional)* — an LLM writes one-line descriptions for brand-new
   fields. It touches nothing else.
6. **Apply** — edit the published file (preserving your hand-written prose and
   structure), reporting any patch that couldn't apply instead of failing
   silently.
7. **PR** — open a pull request. You review and merge.

Steps 2–7 are identical for every project. Only step 1 depends on your stack.

---

## Why it's different

- **Deterministic detection.** Drift is found by mechanically diffing two specs,
  not by asking a model. Results are reproducible and can gate CI.
- **Direction-aware severity.** driftsync knows a removed *response* field breaks
  consumers (BREAKING) while a removed *request* field just means the server
  accepts less (info). Same edit, opposite impact — classified correctly.
- **The LLM proposes, it never decides.** It writes descriptions for new fields.
  If it fails or hallucinates, the structural fix is unaffected — the description
  is simply left blank.
- **Human approval is structural.** The published contract is only ever changed
  via a PR. There is no "auto-commit to your public docs" path.
- **Framework-agnostic core.** Any project that can emit an OpenAPI spec plugs in
  — supporting a new framework is one extraction step, never a core change.

---

## Quick start

### 1. Build the CLI

```bash
go install github.com/karosia/driftsync/cmd/driftsync@latest
```

Or build from source:

```bash
git clone https://github.com/karosia/driftsync
cd driftsync
go build -o driftsync ./cmd/driftsync   # requires Go 1.22+
```

You get one binary with subcommands:

| Command | What it does |
|---------|--------------|
| `driftsync diff <published> <code>`   | detect drift; exits non-zero on breaking |
| `driftsync patch <published> <code>`  | show the proposed JSON Pointer edits |
| `driftsync enrich <published> <code>` | patches + LLM-written descriptions |
| `driftsync apply <published> <code>`  | write the fixed published spec to stdout |
| `driftsync genspec [out]`             | extract a spec from the built-in demo API |

### 2. Get an OpenAPI file out of your code

This is the only stack-specific step. See **[Stack guide](#stack-guide)** below.
The result is a file, e.g. `openapi.gen.yaml`.

### 3. Run it locally

```bash
driftsync diff  docs/openapi.yaml openapi.gen.yaml     # what drifted
driftsync patch docs/openapi.yaml openapi.gen.yaml     # proposed edits
driftsync apply docs/openapi.yaml openapi.gen.yaml > docs/openapi.fixed.yaml
```

### 4. Automate it on every push

Copy a workflow from [`examples/workflows/`](examples/workflows/) into
`.github/workflows/`. See **[Automating with GitHub Actions](#automating-with-github-actions)**.

---

## Stack guide

driftsync compares two OpenAPI files. Your only job is to make your code emit
the first one. Find your stack:

| Your stack | How to extract | Notes |
|------------|----------------|-------|
| **Go + huma** | in-process via `humaadapter` (a ~10-line `tools/genspec`) | emits 3.1 natively |
| **Go + gin + swaggo** | `swag init` → convert 2.0→3.x | compares *annotations* vs docs |
| **Python + FastAPI** | dump `app.openapi()` to a file | emits 3.x |
| **Node + NestJS** | `@nestjs/swagger` document → file | emits 3.x |
| **Java + Spring** | springdoc `/v3/api-docs` → file | emits 3.x |
| **Anything else that emits a spec** | write it to a file | driftsync just reads it |
| **No spec at all** (raw net/http, Express, Flask) | runtime capture or static analysis | future adapters — ask first |

### Go + huma (in-process)

Add a small generator to your repo:

```go
// tools/genspec/main.go
package main

import (
	"context"
	"os"

	"github.com/karosia/driftsync/adapters/humaadapter"
	"your/module/app" // your package that builds the huma.API
)

func main() {
	api := app.NewAPI() // your real API constructor
	doc, err := humaadapter.New(api).Extract(context.Background())
	if err != nil {
		panic(err)
	}
	os.WriteFile("openapi.gen.yaml", doc.Raw, 0o644)
}
```

```bash
go run ./tools/genspec   # -> openapi.gen.yaml
```

This is the *only* place your project imports driftsync, and it only imports the
extractor — none of the diff/patch/LLM machinery ends up in your build.

### Everything else

If your framework can emit a spec, emit it to a file and hand both files to
driftsync. The ready-made workflows in
[`examples/workflows/`](examples/workflows/) show the exact extraction command
for FastAPI, NestJS, Spring, and gin+swaggo.

> **gin + swaggo caveat:** swaggo generates from *annotations*, not the code
> itself, and emits OpenAPI 2.0. Convert 2.0→3.x first (driftsync is 3.x-only),
> and be aware you're comparing annotations against the published doc — stale
> annotations are their own kind of drift.

---

## Automating with GitHub Actions

driftsync is built to run on every push. There are two modes:

### Mode A — Sync + PR

On every push, detect drift, fix the published spec, and **open a pull request**.
Nothing merges automatically — the PR is your approval gate.

```yaml
# .github/workflows/drift-sync.yml  (Go + huma shown; see examples/ for other stacks)
name: API Doc Drift Sync
on:
  push: { branches: [main] }
  workflow_dispatch: {}
permissions:
  contents: write
  pull-requests: write
jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - name: Build driftsync
        run: |
          git clone https://github.com/karosia/driftsync /tmp/driftsync
          (cd /tmp/driftsync && go build -o /usr/local/bin/driftsync ./cmd/driftsync)
      - name: Extract spec from code       # <- your stack's step
        run: go run ./tools/genspec
      - name: Detect drift
        continue-on-error: true
        run: driftsync diff docs/openapi.yaml openapi.gen.yaml
      - name: Apply fixes
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
        run: |
          driftsync apply docs/openapi.yaml openapi.gen.yaml > docs/openapi.new
          mv docs/openapi.new docs/openapi.yaml
          rm -f openapi.gen.yaml
      - name: Open sync PR
        uses: peter-evans/create-pull-request@v6
        with:
          branch: drift/sync-openapi
          title: "docs: sync OpenAPI spec with code"
          commit-message: "docs: sync OpenAPI spec with code (automated)"
          body: "Automated drift fix. Review before merging — check BREAKING items."
          labels: documentation, automated
```

### Mode B — Verify only (gate CI)

On every push/PR, **fail the check** if the docs drifted. No changes, no PR — a
human fixes it. `driftsync diff` exits non-zero on breaking drift, turning the
check red. Add it to branch protection to block merges.

```yaml
# .github/workflows/drift-check.yml
name: API Doc Drift Check
on:
  push: { branches: ['**'] }
  pull_request: {}
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - name: Build driftsync
        run: |
          git clone https://github.com/karosia/driftsync /tmp/driftsync
          (cd /tmp/driftsync && go build -o /usr/local/bin/driftsync ./cmd/driftsync)
      - name: Extract spec from code       # <- your stack's step
        run: go run ./tools/genspec
      - name: Verify docs match code
        run: driftsync diff docs/openapi.yaml openapi.gen.yaml
```

Many teams use both: **verify** on PRs, **sync** on `main`.

### Is the extraction automatic?

Yes — once set up. You define the *extraction command* once (the "Extract spec"
step, specific to your stack). After that, every push runs it automatically along
with the rest of the pipeline. You never run it by hand.

### Optional: LLM descriptions

Add repo secrets `ANTHROPIC_API_KEY` and/or `OPENAI_API_KEY` to have new fields
described by an LLM. Set `ANTHROPIC_MODEL` / `OPENAI_MODEL` to your current model
ids. With no keys, the pipeline still runs; descriptions are just blank.

---

## Code-first vs design-first

This tool defaults to **code-first**: the code is the source of truth, and the
published doc is updated to match. If your team treats the **published spec** as
the contract and code must conform (**design-first**), the fix direction reverses
— you'd flag non-conforming code rather than auto-updating the doc. Detection is
identical either way; only which side you patch changes.

---

## Limits & gotchas (read before trusting it blindly)

- **OpenAPI 3.x only.** 2.0/Swagger must be converted first (e.g.
  `swagger2openapi`).
- **Direction-aware severity covers top-level `$ref` bodies.** Inline or deeply
  nested request/response schemas are still detected, but classified coarsely.
- **Renames** are matched deterministically for case/separator changes
  (`user_id` ↔ `userId`) and near-misses. Semantic renames (`qty` → `quantity`)
  still appear as remove + add.
- **`apply` preserves structure, not exact formatting.** YAML re-marshalling may
  normalize some styling; the PR diff is small but not always minimal.
- **Failed patches are reported, never silent.** If you hand-edited a spot a
  patch targets, that patch is flagged `FAILED` for you to reconcile — the rest
  still apply.
- **The LLM writes descriptions only.** It cannot change which fields are added,
  removed, or retyped.

---

## Project layout

```
driftsync/
├── ir/                 # canonical intermediate representation + Adapter interface
├── adapters/
│   ├── humaadapter/     # Go + huma: in-process extraction
│   └── specfile/        # read a published/generated OpenAPI file
├── canonicalize/       # normalize both specs to 3.1, fold dialect differences
├── diff/               # deterministic, direction-aware drift detection
├── patch/              # drift -> JSON Pointer patch proposals
├── applier/            # apply patches to the published file (partial-failure safe)
├── enrich/             # fill new-field descriptions (delegates to llm/)
├── llm/                # provider-neutral LLM clients (Anthropic, OpenAI) + fallback
├── sampleapi/          # demo huma service (stands in for "your project")
└── cmd/driftsync/      # the CLI (diff / patch / apply / enrich / genspec)
```

---

## FAQ

**Does it need an LLM?** No. The whole detect → patch → apply → PR pipeline runs
deterministically without any key. The LLM only writes descriptions for new
fields.

**Does it push to my public docs automatically?** No. It opens a PR. You merge.

**My project isn't huma — does it still work?** Yes. huma is just one extraction
adapter (an in-process convenience for Go). Any project that can emit an OpenAPI
file works — the code side becomes a file, same as the published side.

**Can it block merges on breaking changes?** Yes. Use the verify-only workflow
and add it to branch protection; `diff` exits non-zero on breaking drift.
