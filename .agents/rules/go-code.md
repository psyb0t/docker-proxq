# Go code

Prefix: GO. Applies to every `.go` file outside `vendor/`.

- **GO1:** `make lint` must pass. It runs `go fix -diff` and golangci-lint with `.golangci.yml`, which enables almost every linter, an 80-column `lll` limit and `wsl_v5`.
- **GO2:** Sentinels live in `internal/config/errors.go` and `pkg/clients/openai/errors.go`. Shared sentinels come from `common-go/errors`, as `validate` does with `ErrRequiredFieldNotSet`.
- **GO3:** New imports must pass the `depguard` allow list in `.golangci.yml`. Adding a dependency means extending that list, then `make dep` to update `go.mod`, `go.sum` and `vendor/`.
- **GO4:** Log through common-go `slogging.GetLogger(ctx)` (falls back to `slog.Default()`), as the handlers in `internal/proxy` do.
- **GO5:** Header names shared with clients are constants in `pkg/types/headers.go`. `internal/proxy/job.go` re-exports them; do not spell them as string literals elsewhere.
