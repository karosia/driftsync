# driftsync

[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-OpenAPI%20DriftSync-blue?logo=github)](https://github.com/marketplace/actions/openapi-driftsync)

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

1. **Extract** — run the `code.command` from your `driftsync.yaml` to get an
   OpenAPI spec out of your code (framework-specific; the only part that depends
   on your stack).
2. **Canonicalize** — normalize both specs to OpenAPI 3.1 and fold
   equivalent-but-differently-written constructs, so dialect differences don't
   look like drift.
3. **Diff** — deterministically compare the two. Severity is **direction-aware**:
   the same change is breaking in a response but harmless in a request, and vice
   versa.
4. **Patch** — turn each drift into a concrete JSON Pointer (RFC 6901) edit,
   with the correct value copied from the code. A rename or removal keeps
   `required` in sync automatically (the old name never dangles), a whole new
   or removed schema is added or dropped (not just its properties), and
   renaming a schema itself rewrites every existing `$ref` to it — not only
   the schema body — so nothing is left pointing at a name that no longer
   exists.
5. **Enrich** *(optional)* — an LLM writes one-line descriptions for brand-new
   fields. It touches nothing else.
6. **Apply** — edit the published file (preserving your hand-written prose and
   structure), reporting any patch that couldn't apply instead of failing
   silently.
7. **PR** — open a pull request. You review and merge.

Steps 2–7 are identical for every project. Only step 1 depends on your stack —
and it's one line of config. `driftsync check` runs 1–4; `driftsync sync` runs
1–6 and writes the fix; step 7 is your CI's PR step.

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

### 1. Install the CLI

```bash
go install github.com/karosia/driftsync/cmd/driftsync@latest   # requires Go 1.25+
```

Prebuilt binaries for Linux / macOS / Windows are attached to each
[release](https://github.com/karosia/driftsync/releases).

### 2. Create a `driftsync.yaml`

`driftsync init` scaffolds one for your stack (`fastapi`, `nestjs`, `spring`,
`huma`, `swaggo`, or `generic`):

```bash
driftsync init --stack fastapi --with-workflows
```

That writes `driftsync.yaml` (and, with `--with-workflows`, two GitHub Actions
workflows). Open `driftsync.yaml` and set two things — where your published spec
lives, and the command that emits the spec from your code:

```yaml
version: 1
published: docs/openapi.yaml          # the spec you maintain
code:
  command: >                          # how to produce the code-side spec
    python -c "import importlib, yaml;
    m, a = 'app.main:app'.split(':');
    app = getattr(importlib.import_module(m), a);
    yaml.safe_dump(app.openapi(), open('openapi.gen.yaml', 'w'), sort_keys=False)"
  file: openapi.gen.yaml              # where the command wrote it
```

Full key reference: **[Configuration](#configuration)**.

### 3. Run it

```bash
driftsync doctor   # sanity-check the setup itself — does code.command run,
                   # do both specs parse as OpenAPI? (no diffing yet)

driftsync check    # extract per driftsync.yaml, diff, print a report,
                   # exit non-zero on breaking drift  (for CI)

driftsync sync     # same, then rewrite docs/openapi.yaml to match the code
                   # and write drift-report.md  (review the change, commit via PR)
```

`doctor` is for onboarding — "is this wired up right" — before you ever get
to "what changed." `check` is the gate; `sync` is the fix. All three read
`driftsync.yaml` — you never pass file paths by hand.

### 4. Automate it

```yaml
# .github/workflows/drift-check.yml
- uses: actions/checkout@v4
- uses: actions/setup-python@v5           # whatever your code.command needs
  with: { python-version: "3.12" }
- run: pip install -r requirements.txt pyyaml
- uses: karosia/driftsync@v1
  with: { mode: check }
```

See **[Automating with GitHub Actions](#automating-with-github-actions)**.

### Lower-level commands

`check` / `sync` cover the normal flow. The pipeline stages are also exposed
directly, taking explicit file paths and no config:

| Command | What it does |
|---------|--------------|
| `driftsync diff <published> <code>`   | detect drift; exits non-zero on breaking |
| `driftsync patch <published> <code>`  | show the proposed JSON Pointer edits |
| `driftsync enrich <published> <code>` | patches + LLM-written descriptions |
| `driftsync apply <published> <code>`  | write the fixed published spec to stdout |
| `driftsync genspec [out]`             | extract a spec from the built-in demo API |

---

## Configuration

`driftsync.yaml` lives at your repo root and is the single input to `check` and
`sync`. Relative paths resolve against the file's directory; CLI flags override
config keys.

```yaml
version: 1                        # required, must be 1

published: docs/openapi.yaml      # required — the spec you publish and maintain

code:                             # required — how to get the spec your code emits
  command: make openapi           #   optional: shell command that (re)generates `file`.
                                  #   Omit if `file` is already produced by an earlier step.
                                  #   Runs with driftsync.yaml's directory as the working dir.
                                  #   Chain conversions here too (e.g. swaggo 2.0 -> 3.x).
  file: openapi.gen.yaml          #   required: path driftsync reads the code spec from

# ---- everything below is optional; values shown are the defaults ----

fail_on: breaking                 # `check` exit policy: breaking | any | never
                                  #   breaking → non-zero only on BREAKING drift
                                  #   any      → non-zero on any drift at all
                                  #   never    → always zero (report only)

enrich: auto                      # `sync` LLM descriptions for new fields:
                                  #   auto → on when ANTHROPIC_API_KEY / OPENAI_API_KEY is set
                                  #   on   → always (offline stub text with no key)
                                  #   off  → never; new-field descriptions stay blank

report: text                      # stdout report format: text | md | json

report_file: drift-report.md      # also write the report here (omit = stdout only)

sync_output: docs/openapi.yaml    # where `sync` writes the fix (omit = overwrite `published`)
```

Flag overrides: `--config`, `--format`, `--fail-on` (check), `--report-file`,
`--output` (sync).

Ready-made configs per stack: [`examples/config/`](examples/config/).

---

## Stack guide

The stack-specific part is one line: the `code.command` in `driftsync.yaml` that
produces an OpenAPI 3.x file. `driftsync init --stack <x>` fills it in; the table
shows what you get.

| `--stack` | `code.command` (abbreviated) | Notes |
|-----------|------------------------------|-------|
| `huma`    | `go run ./tools/genspec` | in-process, emits 3.1 natively; `init` also scaffolds `tools/genspec/main.go` |
| `fastapi` | `python -c "... app.openapi() ..."` | dumps `app.openapi()`; no server |
| `nestjs`  | `npx ts-node scripts/genspec.ts` | `@nestjs/swagger` document; add the small script |
| `spring`  | boot jar → `curl /v3/api-docs.yaml` | springdoc; or use the maven plugin |
| `swaggo`  | `swag init` → `swagger2openapi` | compares *annotations*, emits 2.0 → converted to 3.x (needs `npx`) |
| `generic` | `make openapi` (yours) | anything that writes an OpenAPI 3.x file |

Full example configs: [`examples/config/`](examples/config/).

### Go + huma (in-process)

huma is code-first and emits 3.1 natively. `driftsync init --stack huma`
scaffolds `tools/genspec/main.go`:

```go
// tools/genspec/main.go
package main

import (
	"context"
	"os"

	"github.com/karosia/driftsync/adapters/humaadapter"
	app "your/module/app" // your package that builds the huma.API
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

Edit the import line and constructor; `driftsync.yaml` already runs it via
`go run ./tools/genspec`. This is the *only* place your project imports driftsync,
and it imports only the extractor — none of the diff/patch/LLM machinery ends up
in your build.

### swaggo caveat

swaggo generates from *annotations*, not the code itself, and emits OpenAPI 2.0.
The generated `code.command` converts 2.0 → 3.x with `swagger2openapi` (needs
Node on the runner). You're comparing annotations against the published doc —
stale annotations are their own kind of drift.

---

## Automating with GitHub Actions

The composite action installs driftsync and runs `check` or `sync` against your
`driftsync.yaml`. You supply two things around it: the **toolchain** your
`code.command` needs, and (for sync) the **PR step**.

```yaml
- uses: karosia/driftsync@v1
  with:
    mode: check                 # check | sync            (default: check)
    config: driftsync.yaml      # config path             (default: driftsync.yaml)
    version: latest             # a release tag or latest (default: latest)
    report-file: ""             # also write the report here
    working-directory: "."
```

Copy [`examples/workflows/drift-check.yml`](examples/workflows/drift-check.yml)
and [`drift-sync.yml`](examples/workflows/drift-sync.yml) (or run
`driftsync init --stack <x> --with-workflows`). Many teams use both: **check**
on PRs, **sync** on `main`. Stack-specific `check` variants (toolchain step
pre-wired, nothing to uncomment) are in
[`examples/workflows/`](examples/workflows/) too — driftsync's own install is
the same prebuilt binary either way; only that one step changes per stack.

### Mode A — Sync + PR

On every push to `main`: extract, fix the published spec, write
`drift-report.md`, and **open a pull request**. Nothing merges automatically.

```yaml
# .github/workflows/drift-sync.yml
name: API Doc Drift Sync
on:
  push: { branches: [main] }
  workflow_dispatch: {}
permissions:
  contents: write
  pull-requests: write
jobs:
  drift-sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5           # ← toolchain for your code.command
        with: { go-version-file: go.mod }
      - uses: karosia/driftsync@v1
        with:
          mode: sync
          report-file: drift-report.md
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}   # optional
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}         # optional
      - uses: peter-evans/create-pull-request@v6
        with:
          branch: drift/sync-openapi
          title: "docs: sync OpenAPI spec with code"
          commit-message: "docs: sync OpenAPI spec with code (automated)"
          body-path: drift-report.md
          labels: documentation, automated
```

### Mode B — Verify only (gate CI)

On every push/PR, **fail the check** if the docs drifted. No changes, no PR.
`driftsync check` exits non-zero per `fail_on` in your config (`breaking` by
default). Add the job to branch protection to block merges.

```yaml
# .github/workflows/drift-check.yml
name: API Doc Drift Check
on:
  push: { branches: ['**'] }
  pull_request: {}
jobs:
  drift-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5           # ← toolchain for your code.command
        with: { go-version-file: go.mod }
      - uses: karosia/driftsync@v1
        with: { mode: check }
```

Either mode also writes the report to the run's **Job Summary** tab (in
addition to the normal console output and `report-file`), so the outcome is
visible without opening logs.

### Is the extraction automatic?

Yes. `code.command` in `driftsync.yaml` is defined once; `check` / `sync` run it
every time, in CI and locally. You never run it by hand.

### Optional: LLM descriptions

Add repo secrets `ANTHROPIC_API_KEY` and/or `OPENAI_API_KEY` to have new fields
described by an LLM. Set `ANTHROPIC_MODEL` / `OPENAI_MODEL` to your current model
ids. With no keys, the pipeline still runs; descriptions are just blank.

With a key set, `sync` also asks the LLM about **semantic renames** — a
remove+add pair the deterministic matcher couldn't pair by name (`qty` ->
`quantity`; see [Limits & gotchas](#limits--gotchas)) but that share a schema
and type. Confirmed guesses show up as a "Possible renames" section in the
report/PR body — advisory only, the patches themselves are still the same
plain removal + addition either way.

### Optional: LLM-drafted `code.command`

`driftsync init --stack generic --describe "a Go service; run 'go run
./tools/genspec'"` has an LLM draft the `code.command` line from a prose
description, instead of leaving the `make openapi` placeholder for you to
edit by hand. Needs the same API keys as above. Review the drafted command
before trusting it — it's written, not verified to actually run.

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
  still *patch* as remove + add — with an LLM key set, `sync` will flag an
  unambiguous one as a possible rename in the report, but that's a note for
  the reviewer, not a different patch.
- **`apply` preserves structure, not exact formatting.** YAML re-marshalling may
  normalize some styling; the PR diff is small but not always minimal.
- **Failed patches are reported, never silent.** If you hand-edited a spot a
  patch targets, that patch is flagged `FAILED` for you to reconcile — the rest
  still apply.
- **The LLM writes descriptions only.** It cannot change which fields are added,
  removed, or retyped.
- **A `required` entry naming no property is flagged, not fixed.** driftsync
  reports it (`required_dangling`, info) on whichever side has it — there's no
  code-side value to auto-correct a name that isn't a real property.

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
├── config/             # driftsync.yaml: parse, validate, embedded init templates
├── run/                # orchestration behind `check` / `sync`
├── sampleapi/          # demo huma service (stands in for "your project")
├── cmd/driftsync/      # the CLI (init / check / sync / diff / patch / apply / enrich / genspec)
├── action.yml          # composite GitHub Action (uses: karosia/driftsync@v1)
└── .goreleaser.yaml    # prebuilt release binaries
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

**Can it block merges on breaking changes?** Yes. Use `drift-check.yml` and add
it to branch protection; `driftsync check` exits non-zero per `fail_on`
(`breaking` by default).

**Do I have to use `driftsync.yaml`?** No — `diff` / `patch` / `apply` / `enrich`
take explicit file paths and ignore the config. `check` / `sync` / `doctor` are
the config-driven convenience layer on top.
