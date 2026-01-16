# Contributing to mq

Thank you for your interest in contributing to `mq`! We welcome contributions from everyone.

## Development Workflow

We use `make` to manage the development lifecycle. See 'make help' for more information on the available targets.

### Prerequisites

- Go 1.24+
- `golangci-lint` (for linting)
- `podman` or `docker` (for integration tests)

### Full Check

Before sending a PR, please ensure everything is green by running:

```bash
make full
```

This runs formatting, linting, building, unit tests, integration tests, and benchmarks.

## Coding Style

*   We follow standard Go idioms.
*   All tests must run with `-race` enabled.
*   Performance critical code (e.g. packet decoding) should avoid unnecessary allocations.

## Pull Request Process

1.  Fork the repository.
2.  Create a new branch for your feature or fix.
3.  Add tests for your changes.
4.  Run `make full` to ensure all checks pass.
5.  Submit a Pull Request with a clear description of your changes.
