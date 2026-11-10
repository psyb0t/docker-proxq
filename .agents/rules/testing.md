# Testing

Prefix: TEST. How to run each suite is in `AGENTS.md` and `docs/development.md`.

- **TEST1:** Unit tests sit beside the code. Tests that need Redis start it through `internal/testinfra` (`SetupRedis`, `SetupRedisWithACL`) and are named `*_integration_test.go`. They run in the normal `go test ./...`, so `make test` needs the Docker socket.
- **TEST3:** The coverage floor is 90%, checked by `make test-coverage` over every package except `internal/app`, `internal/testinfra`, `cmd` and `tests`. Raise coverage for new code rather than lowering `MIN_TEST_COVERAGE` or widening the exclusions.
- **TEST4:** `tests/` is a separate Go module of end-to-end tests that builds the image and drives it with testcontainers. CI does not run it. Run it with `cd tests && go test -v -timeout 10m ./...` when a change touches the image or the end-to-end behavior.
