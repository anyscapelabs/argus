# test/

Cross-package tests for the Argus desktop app.

## Layout

- `frontend/` — Vitest suites for the React/TS frontend, mirroring
  `frontend/src` structure (`frontend/lib/*.test.ts` tests `frontend/src/lib/*.ts`
  etc.). Run with `npm test` from `frontend/`, or `task test:frontend`.

Go unit tests stay colocated with their packages (same-package white-box
tests; `go test ./...`, also wired as `task test`).
