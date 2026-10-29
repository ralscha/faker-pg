# faker-pg

A terminal UI for anonymizing sensitive data in PostgreSQL databases.
Connect to a Postgres database, map columns to realistic [gofakeit](https://github.com/brianvoe/gofakeit) generators, then let faker-pg rewrite matching rows in place.
An optional LLM (OpenAI-compatible) can auto-suggest mappings based on column names and types.

```
faker-pg -- PostgreSQL Fake Data Anonymizer
  Status: Done! Updated 84,312 rows across 7 tables.

  ┌─ Column ──────────────────────────────────────┬── Type ────────────────┬── Faker Function ──────────────────┐
  │ public.users.email                            │ character varying      │ Email                              │
  │ public.users.full_name                        │ character varying      │ Name                               │
  │ public.users.phone                            │ character varying      │ PhoneFormatted                     │
  │ public.orders.shipping_address                │ text                   │ Street                             │
  └───────────────────────────────────────────────┴────────────────────────┴────────────────────────────────────┘
```

## Features

- **Interactive TUI** — navigate with keyboard; no config files required
- **Schema discovery** — reads tables, columns, primary keys, and foreign keys from PostgreSQL metadata, including references to unique indexes
- **Column filtering** — include/exclude schemas or tables by name
- **Searchable fake generators** — supported scalar [gofakeit](https://github.com/brianvoe/gofakeit) functions, searchable by name or category
- **Type-aware picker** — shows only generators whose output type is compatible with the target column
- **Parameterised functions** — pass arguments to generators (e.g. `numerify` format strings)
- **Regex selectors** — map a single rule to multiple columns via a pattern
- **Constraint safeguards** — key columns, generated columns, and unsupported types are excluded to preserve integrity
- **Batched parallel execution** — update rows in bounded batches and process independent tables concurrently
- **LLM auto-select** — uses any OpenAI-compatible model to suggest mappings for sensitive columns
- **Persistent cache** — mappings are saved to `~/.faker-pg/fake-data-mapping.yml` and restored on next run
- **Remote host guard** — confirms before updating a remote database, including fallback hosts in multi-host DSNs

## Requirements

- Go 1.27.3+
- PostgreSQL 12+ (accessed over TCP; unix sockets are not tested)
- _(Optional)_ Docker — for the `task dev:db:*` helpers
- _(Optional)_ An OpenAI-compatible API key — for LLM auto-select

## Installation

### Pre-built binaries

Download the latest release for your platform from the [Releases](https://github.com/ralscha/faker-pg/releases) page.
Pre-built binaries are available for Linux, macOS, and Windows (amd64, arm64).

### go install

```bash
go install github.com/ralscha/faker-pg/cmd/faker-pg@latest
```

### Build from source

```bash
git clone https://github.com/ralscha/faker-pg
cd faker-pg
go build -o faker-pg ./cmd/faker-pg
```

## Quick start

```bash
# Launch the TUI — fill in connection details interactively
faker-pg

# Pre-fill the DSN from the command line
faker-pg --dsn "postgres://user:pass@localhost:5432/mydb?sslmode=disable"

# Supply reusable rules from the command line (the flag is repeatable)
faker-pg \
  --dsn "postgres://user:pass@localhost:5432/mydb?sslmode=disable" \
  --fake-data "email=email" \
  --fake-data 'public\..*\.phone=phone'

# Pre-fill the DSN and configure the LLM for auto-select
faker-pg \
  --dsn "postgres://user:pass@localhost:5432/mydb?sslmode=disable" \
  --llm-model "gpt-4o-mini" \
  --llm-api-key-env OPENAI_API_KEY
```

### Demo database

Spin up a local Postgres container with the demo schema:

```bash
task demo:setup
# Connection DSN: postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable
```

Tear it down when done:

```bash
task dev:db:down
```

## TUI walkthrough

### 1 — Connection form

Fill in the PostgreSQL connection details and optional LLM configuration, then choose an action:

| Key | Action |
|---|---|
| `Tab` / `↑↓` | Move between fields |
| `^F` | Load schema and open the fake-data editor |
| `^A` | Start anonymization immediately (uses configured or cached rules) |
| `Ctrl+C` | Quit |

**Schema filters** (comma-separated):

| Field | Effect |
|---|---|
| Include schemas | Only process tables in these schemas |
| Exclude schemas | Skip tables in these schemas |
| Include tables | Only process these tables (bare name or `schema.table`) |
| Exclude tables | Skip these tables |

### 2 — Fake-data editor

A table listing every copyable column and its currently assigned faker function.

| Key | Action |
|---|---|
| `↑` / `↓` | Navigate rows |
| `Enter` | Open the function picker for the selected column |
| `X` / `Delete` | Clear the current function assignment |
| `A` | Auto-select mappings with the configured LLM |
| `Q` | Save mappings to cache and return to the connection form |

### 3 — Function picker

Type to filter the 600+ available gofakeit functions. Only functions whose output type is compatible with the selected column's data type are shown.

| Key | Action |
|---|---|
| Type | Filter by name, category, or description |
| `↑` / `↓` | Navigate |
| `Enter` | Select the highlighted function |
| `Esc` | Cancel |

If the selected function accepts parameters, a parameter entry screen is shown next. Enter multiple values in the displayed order, separated by `;`.

### 4 — Run

Press `^A` from the connection form to start. faker-pg will:

1. Parse the fake-data rules
2. Connect to PostgreSQL and load the current schema
3. For each table that has at least one mapped column, replace values in bounded batches within one transaction per table
4. Process up to `--workers` tables concurrently
5. Report the total rows and tables updated

A confirmation screen is shown before touching any non-localhost host.

Each table is committed atomically. A failure rolls back that table, including earlier batches; tables already committed remain changed. Quitting cancels outstanding database and LLM work. Press `Enter` or `Esc` on the results screen to return to the connection form.

Table filters and mappings apply to physical tables. Inherited child tables and partition leaves are processed separately, so excluding a child prevents its rows from being changed through a parent. For partitioned tables, select the leaf partitions; explicitly including a storage-free partition root reports an error explaining this requirement.

## CLI flags

| Flag | Default | Description |
|---|---|---|
| `--dsn` | _(empty)_ | PostgreSQL DSN (`postgres://user:pass@host:port/db?sslmode=disable`) |
| `--include-schemas` | _(all)_ | Comma-separated schema names to include |
| `--exclude-schemas` | _(none)_ | Comma-separated schema names to exclude |
| `--include-tables` | _(all)_ | Comma-separated table names to include |
| `--exclude-tables` | _(none)_ | Comma-separated table names to exclude |
| `--fake-data` | _(none)_ | Repeatable `selector=function[;parameter...]` rule |
| `--batch-size` | `1000` | Rows sent in each database update batch |
| `--workers` | `1` | Maximum number of tables processed concurrently |
| `--llm-provider` | `openai` | LLM provider (currently only `openai`-compatible) |
| `--llm-model` | _(empty)_ | Model name, e.g. `gpt-4o-mini` |
| `--llm-base-url` | _(empty)_ | Override API base URL (e.g. for Ollama or a proxy) |
| `--llm-api-key` | _(empty)_ | Inline API key (prefer `--llm-api-key-env` instead) |
| `--llm-api-key-env` | `OPENAI_API_KEY` | Environment variable to read the API key from |
| `--verbose` | `false` | Enable verbose logging |
| `--version` | `false` | Print the build version and exit |

`--batch-size` and `--workers` must be positive integers. Invalid values and unexpected positional arguments are rejected before the TUI starts.

## Fake-data selectors

Rules are matched from most to least specific:

| Selector form | Example | Matches |
|---|---|---|
| `schema.table.column` | `public.users.email` | Exactly that column |
| `table.column` | `users.email` | Column `email` in any `users` table across all schemas |
| `column` | `email` | Any column named `email` in any table |
| Regex | `public\..*\.email` | Any column whose full name matches the pattern |

Selectors are case-insensitive. Quoted identifiers (`"MyTable"`) are normalised to lower-case.
Use `--fake-data` to configure generic and regex selectors; rules selected in the editor are stored as exact selectors.
Regex selectors match the entire qualified column name, including every branch of an alternation; escapes such as `\D` and `\S` retain their regular-expression meaning.

### Function parameters

Append parameters with `;` separators:

```
numerify;###-###-####
```

## LLM auto-select

When a model and API key are configured, pressing `A` in the fake-data editor sends column names and types to the model and applies its suggestions.

Any OpenAI-compatible endpoint works (Ollama, Azure OpenAI, LiteLLM, etc.) via `--llm-base-url`.

## Cache

Mappings are persisted to `~/.faker-pg/fake-data-mapping.yml` keyed by `host:port/database`. On the next run against the same database, the editor is pre-populated with your previous choices and `^A` can start from cached rules. Existing `host/database` cache entries remain readable.

## Development

```bash
# Run tests
task test

# Run tests with coverage report
task test:cover

# Run integration tests in a disposable PostgreSQL database
task test:integration TEST_DSN="postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable"

# Format code
task format

# Run linter
task lint

# Build binary
task build

# Run directly (supports DSN, LLM_MODEL, LLM_KEY_ENV variables)
task run DSN="postgres://postgres:postgres@localhost:5432/devdb?sslmode=disable"
```

The integration suite creates and removes isolated test schemas. Plain `go test ./...` skips these tests unless `FAKER_PG_TEST_DSN` is set. CI runs them against PostgreSQL with the Go race detector enabled.

## License

[MIT](LICENSE)
