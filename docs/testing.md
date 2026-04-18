# Testing strategy

This project uses fast unit tests with mocked orchestration boundaries.

## Coverage areas

- Xray config rendering and validation (`internal/xraycfg`)
- model validation (`internal/model`)
- deploy orchestration sequencing (`internal/deploy`)
- SSH command and copy behavior (`internal/ssh`)
- stats and quota logic (`internal/stats`)
- local and remote SQLite stores (`internal/store/local`, `internal/store/remote`)
- CLI behavior and flag validation (`internal/cli`)
- logging redaction and sensitive output filtering (`internal/logx`)

## Out of scope for unit tests

- real SSH sessions
- real remote hosts
- real Docker daemon interactions
- real Xray runtime processes

These are validated by runtime commands during operator workflows.

## Local test commands

```bash
go test ./...
go test -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go test -race ./...
```

Makefile shortcuts:

```bash
make test
make test-cover
make test-race
```

## CI expectations

- `ci.yml` runs tests, coverage, lint, and config checks
- machine-readable test output is captured for artifacts
- ansible syntax/lint checks run in CI
