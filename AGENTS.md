# AGENTS.md

This file provides guidance to the AI agent when working with code in this repository.

## Language

All comments, doc strings, and user-facing messages are written in **Chinese**. Code identifiers remain English. Follow this convention when adding or modifying code.

## Build & Test

Standard Go tooling; no Makefile or custom scripts.

```sh
go build ./...
go test ./...
```

Integration tests in `zcdb/` require live database connections and are in files named `*_integration_test.go`. Run them only when a real DB is available; they are skipped in normal `go test ./...` runs if connection env vars are absent.

Reset global state before each test that touches `zcconfig`:

```go
zcconfig.Reset()
```

## Testing Conventions

- Standard library `testing` package only — no testify, gomock, or other third-party test frameworks.
- Table-driven tests preferred.
- Test names follow `TestPackageName_Scenario` (e.g., `TestMySQLGrammar_SelectBasic`).
- HTTP handler tests use `httptest.NewRequest` + `httptest.NewRecorder` and unwrap responses via the `{data, code, message}` envelope helper `decodeData()`.

## zchttp Handler Contract

Every route handler **must** have this exact signature:

```go
func(ctx context.Context, req Req) (Res, error)
```

Where `Req` and `Res` are structs or struct pointers. Any other signature causes a panic at registration time (reflection pre-computation runs at startup, not per request).

## zchttp Struct Tags

The following tags are used by the framework and must be correct:

| Tag | Package | Purpose |
|---|---|---|
| `json` | zchttp binding | JSON field name |
| `form` | zchttp binding | Form/query field name (takes priority over `json`) |
| `nonzero:"true"` | zchttp binding | Required-field validation |
| `default:"value"` | zchttp binding | Default value if field is zero |
| `time_format` | zchttp binding | Time parsing layout or `"unix"` |
| `time_location` | zchttp binding | IANA timezone name for time parsing |
| `example` | zchttp openapi | OpenAPI example value |
| `description` | zchttp openapi | OpenAPI field description |
| `ignore:"true"` | zchttp openapi | Exclude field from OpenAPI schema |
| `tags` / `summary` | zchttp openapi | Embed in `OpenAPIMeta` struct inside Req |
| `db` | zcdb reflect | Column name for SQL builder struct mapping |

## zcdb Grammar Pattern

`Builder` accumulates query state; `Grammar` compiles it to SQL. The three dialects are `MySQLGrammar`, `PostgresGrammar`, `SQLiteGrammar`. PostgreSQL uses `$1`-style placeholders; MySQL/SQLite use `?`. Do not mix placeholder styles.

## Code Generation

`zc gen:model` generates Entity/DO struct pairs with `ToDO()`/`ToEntity()` methods using AST rewriting that preserves user-defined methods. Generated files are in `zc/model/test/` as examples. Do not hand-edit generated output files.
