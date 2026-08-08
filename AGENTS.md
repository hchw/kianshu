# Repository Guidelines

## Project Overview

Kianshu (鉴枢) is an AI-driven integration-testing platform. Users import a Swagger/OpenAPI document to generate test units, then an LLM agent produces **executable, human-editable tree-shaped test flows** ("flow trees"). Each tree is made of typed nodes (start, api, assert, loop, try/catch, cache-set, adapter), supports draft trial runs, immutable version snapshots, and cron scheduling.

Design invariants (from `tsc.md` / `README.md` — do not violate):
- **Tree + shared-cache bypass**: one tree, one start node per flow; cross-layer data passes through an explicit shared cache.
- **Predictable execution**: a node failure stops only its subtree; try/catch contains exceptions (no bubbling); loop iterates element-wise and aggregates output.
- **Self-contained version snapshots**: saving-enabled flows produce immutable versions; history/logs replay exactly without depending on current Swagger state.
- **LLM agent edit loop**: 50 tool-call rounds max, real-time SSE push per round, validation-failure retries.

Stack: Go + Gin + GORM (SQLite default / MySQL / Postgres) backend; React + Vite + TypeScript frontend in `web/` (canvas via `@xyflow/react`); LLM via user-managed OpenAI-compatible providers; gocron scheduling.

## Architecture & Data Flow

Layered monorepo: HTTP layer → service layer → domain layer, with cross-cutting packages.

```
cmd/server (entry) → internal/httpapi (Gin handlers)
                       ↓
                   internal/service (business logic; stateless funcs taking *gorm.DB as first arg)
                       ↓
                   internal/model (GORM models) · internal/flow (tree model + validation) · internal/exec (runner)
cross-cutting: internal/config, internal/db, internal/crypto, internal/openai, internal/scheduler, internal/jsonata
```

- **Trial run / version run**: `httpapi` handler → `service.TrialRun`/`service.RunVersion` (`internal/service/run.go`) → `flow.ParseTree` → `exec.Run(Options{CallAPI: HTTPCallAPI, Host, Timeout})` (`internal/exec/exec.go`) → depth-first tree traversal → results logged to `model.ExecutionLog`.
- **LLM flow generation/editing**: `httpapi/agent.go` → `service.RunAgent`/`GenerateFlow` (`internal/service/generate.go`, `agent.go`) via `ChatProvider` interface → `openai.Client.ChatCompletion` → tools in `internal/service/agent_tools.go` (`ExecTool`: list/filter_units, create/update/delete_node, link_nodes, validate_flow) mutate the tree → `SaveAndEnable` persists a `FlowVersion`. Progress streams to the browser over SSE (`/agent/submit`).
- **Frontend ↔ backend**: axios client at base `/api` (Vite dev-proxies to `:8080`). Bearer token injected by request interceptor; 401 → redirect `/login`.

## Key Directories

| Path | Purpose |
|------|---------|
| `cmd/server/` | Single binary entry (`main.go`, swagger annotations in `swagger.go`) |
| `internal/httpapi/` | Gin handlers, routing (`server.go`), auth middleware, SSE agent endpoints. One file per resource: `testset.go`, `flow.go`, `units.go`, `provider.go`, `schedule.go`, `members.go`, `auth.go`, `agent.go` |
| `internal/service/` | Business logic: `flow.go` (draft/version lifecycle), `run.go` (execution), `import.go` + `swagger.go` (OpenAPI parsing), `generate.go`/`agent.go`/`agent_tools.go`/`agent_session.go` (LLM), `schedule.go`, `permission.go` |
| `internal/flow/` | Tree model (`tree.go`: node types, `ParseTree`, `ValidateTreeShape`, `CacheKey`) and `validate.go` (`Validate`: single root, acyclicity, I/O contracts, JSONata syntax, try/catch pairing, loop input, cache keys) |
| `internal/exec/` | Execution engine: `exec.go` (`Run`, `Options`, `NodeResult`, `RunResult`), `semantics.go` (try/catch, loop aggregation, cache writes) |
| `internal/model/` | All GORM models + `AutoMigrate` — the schema source of truth |
| `internal/config/` | Env-based config: `Config` struct, `Load()`, sentinel `ErrMissingEncKey` |
| `internal/crypto/` | `AESCipher` (AES-GCM) for encrypting provider API keys |
| `internal/openai/` | `Provider` interface + OpenAI-compatible `Client` (`ChatCompletion`, `Test`) |
| `internal/scheduler/` | `Scheduler` interface (`Start`/`Stop`/`Schedule`/`ValidateCron`/…) + `gocron.go` default impl |
| `internal/jsonata/` | `Parse`/`Eval`/`Exts` JSONata wrapper (adapter + encryption extensions) |
| `web/src/pages/` | Route pages: `Login`, `Register`, `TestSetList`, `TestSetDetail`, `FlowEditor` (core editor), `Providers` |
| `web/src/components/` | Feature components: `testset/` (MembersPanel, UnitsBrowser, ImportPanel), `results/` (SchedulePanel, ResultsPanel), `canvas/` (FlowCanvas, NodePanel), `dialog/` (AgentDialog), `layout/` (AppLayout) |
| `web/src/api/` | Typed axios wrappers per resource: `client.ts`, `auth.ts`, `testset.ts`, `flow.ts`, `providers.ts`, `schedule.ts`, `agent.ts` |
| `web/src/lib/`, `web/src/store/`, `web/src/sse.ts` | Pure tree logic mirroring backend rules; session singleton; hand-rolled SSE consumer |
| `docs/` | Generated Swagger 2.0 (`swagger.yaml` is the source; `swagger.json`/`docs.go` generated) |

## Development Commands

All commands from the repo root, via `Makefile` (which exports `KS_ENC_KEY`; default `dev-only-32-byte-secret-key-0000`):

```bash
make dev              # backend (go run ./cmd/server/main.go) + frontend (npm run dev) together
make dev-backend      # backend only
make dev-frontend     # frontend only (web/, Vite on :5173, /api proxied to :8080)
make build            # go build -o server ./cmd/server && cd web && npm run build
make test             # go test ./... && cd web && npm test
make lint             # go vet ./... && cd web && npm run lint
make clean            # rm -f server kianshu.db
make swagger          # swag init -g cmd/server/swagger.go -o docs --ot go,json,yaml (needs `swag` installed)
```

Backend env config comes from environment variables (see `internal/config/config.go` — `KS_ENC_KEY` is required; SQLite is the default DB driver). Swagger UI at `http://localhost:8080/swagger/index.html`.

## Code Conventions & Common Patterns

- **Language**: All Go comments, code identifiers, and user-facing strings are **Chinese** (README, tsc.md, prompts). Match that in new backend code. `web/` UI text is also Chinese.
- **Backend layering**: `internal/service` functions are **stateless** — take `*gorm.DB` (or a dependency) as the first parameter, e.g. `service.CreateFlow(db, ...)`. Stateful collaborators are structs like `ScheduleManager`. Handlers in `httpapi` may query the DB directly for simple reads.
- **Error handling**: sentinel errors (`errors.Is`-style), e.g. `ErrMissingEncKey`, `NeedConfirmationError` — do not invent ad-hoc error strings; return typed errors so handlers can map them to HTTP status codes.
- **Auth/permissions**: bearer token = sha256 hash of a 32-byte hex token (`newToken`/`hashToken` in `internal/httpapi/util.go`), checked by `withAuth` middleware; per-resource access via `service.AccessRole`/`CanRead`/`CanEdit` (owner / read-only / edit roles on TestSet members).
- **DB**: GORM only, no raw SQL; models centralized in `internal/model/model.go` with `AutoMigrate` at startup. TestUnit dedup key is `method + path` (slash → dash).
- **Frontend state**: module-level singleton, **no Redux/zustand/context** — `web/src/store/session.ts` persists to `localStorage` (`kianshu_token`, `kianshu_user`).
- **Frontend API pattern**: per-resource module exporting typed interfaces + async functions over the shared axios `api` instance; template-literal URLs, `{ data }` destructuring, errors via exported `apiError(e)` helper.
- **Flow logic duplication is intentional**: `web/src/lib/tree.ts` (`parseTree`, `layoutTree`, `reconcileChildren`, `validateTreeShape`, `linkAllowed`) mirrors backend validation — keep both in sync when tree semantics change.
- **Styling**: single global stylesheet `web/src/index.css` with semantic class names (`card`, `row`, `mono`, `err`, `.tab.on`) and CSS custom properties. No CSS modules / Tailwind.
- **Naming**: Go — conventional; files map to resources (`internal/httpapi/flow.go`). Frontend — PascalCase components (`FlowEditor.tsx`), camelCase functions; API modules match backend resources.
- **Versioning model**: flows keep a mutable draft; `SaveAndEnable` produces an immutable `FlowVersion`. Never mutate a saved version.

## Important Files

| File | Why |
|------|-----|
| `cmd/server/main.go` | Wiring order: config → db → AutoMigrate → httpapi.New → Routes().Run |
| `internal/httpapi/server.go` | Server struct (holds DB/Cfg/Cipher/LLM/Schedules), `Routes()` registers everything + `withAuth` |
| `internal/model/model.go` | Complete GORM schema; read before touching any DB-adjacent code |
| `internal/flow/tree.go`, `internal/flow/validate.go` | The tree model and validation rules — the domain core; most bugs live here |
| `internal/exec/exec.go`, `internal/exec/semantics.go` | Execution semantics (subtree failure, try/catch, loop aggregation) |
| `internal/service/run.go`, `internal/service/flow.go` | Flow lifecycle: drafts, versions, trial runs |
| `internal/service/agent.go`, `agent_tools.go`, `agent_session.go` | LLM agent loop, tool implementations, per-flow session lock |
| `internal/httpapi/agent.go` | SSE streaming protocol (`/agent/submit`, `[DONE]` terminator) — contract shared with `web/src/sse.ts` |
| `web/src/api/client.ts` | Single axios instance: base URL, token injection, 401 handling |
| `web/src/pages/FlowEditor.tsx` | Core editor wiring canvas + agent dialog + panels + validation |
| `docs/swagger.yaml` | API contract (Swagger 2.0, basePath `/api`); regenerate with `make swagger` after API changes |
| `tsc.md` | Original Chinese requirements/design decisions (T1–T6) — product intent reference |

## Runtime/Tooling Preferences

- **Backend**: Go. `go.mod` declares `go 1.26.5` (module path `github/hchw/kianshu` — non-standard, keep it). Makefile build/test/lint pin `GOTOOLCHAIN=go1.25.6`; plain `go run ./cmd/server` uses the local toolchain.
- **Frontend**: Node + **npm** (`web/package-lock.json` — never use yarn/pnpm). React 19, TypeScript ~6.0 (strict, `tsconfig.app.json` target `es2023`, `jsx: react-jsx`), Vite 8, `@xyflow/react` canvas.
- **Linting**: oxlint only (`.oxlintrc.json`: react/typescript/oxc plugins; `rules-of-hooks` error, `only-export-components` warn) + `go vet`. No eslint/prettier/golangci-lint.
- **Ports**: backend `:8080`, frontend dev `:5173` (proxies `/api` → `:8080`). Vite proxy is the only CORS bridge — no CORS middleware.
- **Spec workflow**: `openspec/` scaffold (spec-driven changes) exists but is currently **empty** — treat it as opt-in, not required.
- **Gotchas**: `server` is the gitignored build artifact; the root `main` binary is NOT produced by the Makefile and NOT gitignored (do not touch it). `.codegraph/` is local indexing, gitignored. `web/README.md` is untouched Vite boilerplate — ignore it.

## Testing & QA

- **Backend**: standard `go test ./...` (Makefile `test` target, pinned toolchain). Table-driven tests colocated as `*_test.go` (see `internal/config/config_test.go`, `internal/service/import_test.go`, `internal/flow/validate_test.go`). Tests reference config env conventions (`config.Load`), so env-driven behavior is testable.
- **Frontend**: **Vitest** with `jsdom` environment (configured in `web/vite.config.ts`); tests colocated as `*.test.ts(x)` next to sources (`api/client.test.ts`, `lib/tree.test.ts`, `components/results/ResultsPanel.test.ts`, `components/canvas/NodePanel.test.ts`, `sse.test.ts`). `sse.ts` and `lib/tree.ts` deliberately export pure functions (`parseSSELine`, `validateTreeShape`) to make them unit-testable.
- **Coverage**: no coverage thresholds or CI pipeline configured. Follow existing test style; keep tests deterministic and colocated.
- **Smoke check**: `make dev`, hit `http://localhost:5173`, register/login, import a Swagger doc, generate/edit a flow.
