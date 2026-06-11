# Contributing

## Development

- `make build`
- `make test`
- `go test ./...`

## Rules

- do not commit `config/values.yaml`
- do not commit `providers/*.yaml`
- do not commit generated runtime configs
- do not add scenario-specific private operations to the public CLI

## Pull requests

- keep changes focused on one logical change
- add or update tests for behavior changes
- update docs when command behavior or config expectations change
