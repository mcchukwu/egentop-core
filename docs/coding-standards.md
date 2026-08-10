# Coding Standards

## Language & Tooling

- Go 1.26+. Format code with `gofmt`; run `go vet ./...` before pushing.
- Follow the official Go style guide and the conventions already present in the
  codebase. When editing a file, mirror its existing structure.
- Package names are short and lowercase (`project`, `membership`), matching
  their directory name under `internal/`.

## Architecture & Layering

- **Handlers** only parse/validate requests, extract IDs from context, call the
  service, and write a response. No SQL, no business rules.
- **Services** hold business rules, orchestrate repositories, enforce tenant
  scope, and run transactions. No HTTP concerns.
- **Repositories** own SQL and map rows to models. No HTTP or business rules.
- DTOs (`dto.go`) carry request payloads with `validate` tags; models
  (`model.go`) are the read/write domain types with JSON tags.
- Cross-feature calls go through the service layer, never directly into another
  package's repository.

## Errors

- Use the sentinel errors in `internal/apperrors` instead of ad-hoc
  `errors.New` in services. Wrap/replace database errors with a sentinel before
  returning them to the handler.
- Add HTTP mappings for any new sentinel in `internal/response/errors.go`.
- Only return errors from handlers via `response.HandleError` or
  `response.ValidationError`.

## Validation

- Put `validate` tags on DTO fields and run them through
  `validation.ValidateStruct` at the top of the handler. Do not re-validate in
  the service.
- Register any custom rules (e.g. Nigerian phone numbers) in
  `internal/validation`.

## Transactions & SQL

- Mutations that write to multiple tables must use `db.WithTransaction`.
- Always scope queries by the tenant's `organization_id` from the request
  context. Never accept a foreign `{id}` from the client and query without a
  tenant filter.
- Use parameterized queries; never interpolate user input.
- Use `db.IsUniqueViolation` / `db.IsUniqueConstraintViolation` /
  `db.IsForeignKeyViolation` to detect Postgres constraint errors.
- When retrying inside a transaction (e.g. slug collisions), use savepoints —
  a unique violation aborts the whole transaction in Postgres.

## Context & Timeouts

- Use `db.WithDBTimeout(ctx)` for every query context.
- Carry user/org/session/role IDs via `internal/requestctx` helpers; never
  reach into raw context values with string keys.
- Prefer `*time.Time` over `time.Time` for optional timestamps, and never
  round-trip through `time.Time.String()`.

## Concurrency & State

- The in-memory rate limiter is not safe for horizontal scale; keep that
  awareness in code comments and docs.
- Prefer immutable/plain data models; keep mutation logic in services.

## Tests

- Add tests alongside the package (see `*_test.go` files).
- Integration tests (`*_integration_test.go`) run against real PostgreSQL via
  `EGTEST_DB_URL`; keep them skippable when the env var is absent
  (`t.Skip`).
- When a test truncates tables, remember `TRUNCATE organizations CASCADE`
  wipes `roles` — reset the schema and re-apply all up migrations instead.

## Naming

- HTTP routes: lowercase, RESTful, plural resources
  (`/v1/orgs/{orgID}/projects/{projectID}/milestones`).
- JSON fields: `snake_case`.
- Permission keys: `resource.action` (e.g. `project.update`,
  `member.remove`).
- Activity/audit actions: `resource.action` past tense for audit
  (`organization.updated`), present tense for activity feed
  (`project.created`).
