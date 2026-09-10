# examples

Copy-paste starting points. `driftsync init --stack <stack> --with-workflows`
writes the same files into your repo.

## `config/`

One `driftsync.yaml` per stack. Drop it at your repo root, edit the
`published:` path and the `code.command`, then run `driftsync check`.

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

Two stack-agnostic GitHub Actions workflows built on the composite action
(`uses: karosia/driftsync@v1`):

| File | What it does |
|------|--------------|
| `drift-check.yml` | fails CI when the docs have drifted (gate merges) |
| `drift-sync.yml`  | rewrites the published spec and opens a PR |

Both need a toolchain-setup step for whatever your `code.command` runs — the
files have the common ones (`setup-go` / `setup-python` / `setup-node` /
`setup-java`) commented in; keep the one you need.
