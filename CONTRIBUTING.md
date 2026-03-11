# Contributing

This project is in **early alpha** and the API is evolving rapidly. We are not accepting pull requests at this time, but issues and bug reports are very welcome.

Issues and bug reports are welcome via the [GitHub issue tracker](https://github.com/kaasops/dataprepper-operator/issues).

## Development Setup

### Prerequisites

- Go 1.26+
- Docker
- kubectl
- [kind](https://kind.sigs.k8s.io/) (for local testing)

### Build and Test

```bash
# Generate code, manifests, build, and run tests
make generate manifests build test
```

### Code Style

- Lint: `golangci-lint run --no-config --enable revive,goconst,prealloc,staticcheck,unparam,gocyclo ./...`
- Table-driven tests with [testify](https://github.com/stretchr/testify)
- Follow [kubebuilder](https://book.kubebuilder.io/) conventions
- Context propagation and wrapped errors: `fmt.Errorf("context: %w", err)`
