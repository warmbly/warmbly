# Upstream maintenance notes (CONFENGE outreach)

Fork: `tjsasakifln/warmbly` · Upstream: `warmbly/warmbly`

## Files introduced (PR1)

| Area | Paths |
| --- | --- |
| Migration | `internal/infrastructure/db/migrations/000083_outreach_staging.{up,down}.sql` |
| Models | `internal/models/outreach.go`; audit entity constants in `audit.go` |
| App | `internal/app/confenge/*` |
| Repository | `internal/repository/pg_outreach.go` |
| API | `internal/api/handler/confenge.go`; routes in `routes.go`; `handler.go` field |
| Wire | `cmd/backend/main.go` (config load + service) |
| Config sample | `deploy/config/env.example` |
| Realtime spine | `web/src/hooks/useRealtimeEvents.ts` (`outreach_*` keys) |
| Docs | `docs/confenge/*` |

## Extension points used

- Feature flag via env (no change to billing/feature gate matrix)
- Org-scoped routes with existing `RequireAccess` / contact permissions
- Audit spine (`AuditEntityOutreachImportRun`, `AuditEntityOutreachAccount`)
- Embedded migrations (same as other features)
- SSRF client (`internal/pkg/safehttp`) for remote feeds

## Probable upstream conflicts

- Migration number `000080` if upstream adds migrations in the same band — renumber before merge
- `cmd/backend/main.go` and `handler.go` are high-churn; keep confenge wiring in a tight block
- `routes.go` advisor/contacts region

## Update strategy

1. Fetch upstream `main`, rebase or merge carefully.
2. If migration collision: renumber `000080` and keep down migration paired.
3. Re-run `gofmt`, `make lint`, `go test ./internal/app/confenge/`.
4. Keep confenge packages self-contained; avoid editing campaign/send cores.

## Disable

Set `CONFENGE_OUTREACH_ENABLED=false` (default). Routes still expose
`GET /confenge/status` with `"enabled": false`. Import/list return not found.

## Remove customization

1. Drop env vars and docs.
2. Delete `internal/app/confenge`, handler, repo, models, migration (new down).
3. Remove wiring in `main.go` / `handler.go` / `routes.go` / realtime spine.

Do not leave staging tables with PII if decommissioning a deployment: export then
drop via down migration after backup.
