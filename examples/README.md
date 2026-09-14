# examples

Copy-paste starting points. `driftsync init --stack <stack> --with-workflows`
writes the same files into your repo.

## `config/`

One `driftsync.yaml` per stack. Drop it at your repo root, edit the
`published:` path and the `code.command`, then run `driftsync doctor` to
verify the setup and `driftsync check` to see any drift.

| File | Stack |
|------|-------|
| `generic.driftsync.yaml` | your build already emits an OpenAPI file |
| `fastapi.driftsync.yaml` | Python + FastAPI (`app.openapi()`) |
| `nestjs.driftsync.yaml`  | Node + NestJS (`@nestjs/swagger`) |
| `spring.driftsync.yaml`  | Java + Spring (springdoc `/v3/api-docs`) |
| `huma.driftsync.yaml`    | Go + huma (in-process via `tools/genspec`) |
| `swaggo.driftsync.yaml`  | Go + gin + swaggo annotations (2.0 → 3.x) |

See the [Configuration](../README.md#configuration) section for every key.

## `workflows/`

GitHub Actions workflows built on the composite action
(`uses: karosia/driftsync@v1`) — driftsync itself installs as a prebuilt
binary regardless of your project's language; only the toolchain step that
runs your `code.command` needs to match your stack.

| File | What it does |
|------|--------------|
| `drift-check.yml` | fails CI when the docs have drifted (gate merges) — stack-agnostic, with the common toolchain steps (`setup-go` / `setup-python` / `setup-node` / `setup-java`) commented in; keep the one you need |
| `drift-sync.yml`  | rewrites the published spec and opens a PR — same toolchain-step convention |
| `drift-check-fastapi.yml` | check, pre-wired for Python + FastAPI |
| `drift-check-nestjs.yml`  | check, pre-wired for Node + NestJS |
| `drift-check-spring.yml`  | check, pre-wired for Java + Spring (springdoc) |
| `drift-check-swaggo.yml`  | check, pre-wired for Go + gin + swaggo (needs both Go and Node) |

The stack-specific files only differ from `drift-check.yml` in that one
toolchain step — for `sync` on one of those stacks, copy its toolchain step
into `drift-sync.yml`. (There's no `-huma` or `-generic` variant: the plain
`drift-check.yml` already works for huma as-is, and "generic" has no fixed
toolchain to pre-wire.)
