# Configuration

Prefix: CFG. The config is one YAML file parsed by `internal/config/config.go`.

- **CFG1:** A new field gets a YAML tag in camelCase, a `Default*` constant and a line in the matching `apply*Defaults` function when it has a default, and validation in `validate` when a bad value should stop startup.
- **CFG2:** A zero value means "use the default" for every defaulted field, because `apply*Defaults` checks `== 0` or `== ""`. A field where zero must mean something else (off, unlimited) needs explicit handling and a test, not a reused default check.
- **CFG3:** A config change updates, in the same change: `config.yaml.example`, `docs/configuration.md`, the tables in `.agents/skills/proxq/references/setup.md`, and `internal/config/config_test.go`. A new per-upstream field is also copied through `buildUpstreamConfigs` in `internal/app/app.go` and `UpstreamConfig` in `internal/proxy/handler.go`.
- **CFG4:** Startup refusals are sentinel errors in `internal/config/errors.go`. A new refusal adds its message to `docs/troubleshooting.md`, copied exactly.
- **CFG5:** Durations are `config.Duration`, parsed from Go duration strings. Do not add integer second fields.
