# Contributing to kbox

Thank you for your interest in contributing to kbox! This document provides guidelines and information for contributors.

## Development Setup

### Prerequisites

- Go 1.21 or later
- Docker
- kubectl
- A local Kubernetes cluster (kind or minikube recommended)

### Building from Source

```bash
git clone https://github.com/bobbyrathoree/kbox.git
cd kbox
go build -o kbox ./cmd/kbox
```

### Running Tests

```bash
# Unit tests
go test ./...

# With verbose output
go test -v ./...

# Specific package
go test -v ./internal/render/...
```

### Local Testing

```bash
# Create a kind cluster
kind create cluster --name kbox-dev

# Build and test
go build -o kbox ./cmd/kbox
./kbox doctor
```

## Project Structure

```
kbox/
├── cmd/kbox/           # CLI entrypoint
├── internal/
│   ├── cli/            # Command implementations
│   ├── config/         # Configuration loading and validation
│   ├── render/         # Kubernetes manifest generation
│   ├── apply/          # Server-Side Apply engine
│   ├── k8s/            # Kubernetes client utilities
│   ├── dependencies/   # Database dependency templates
│   ├── secrets/        # Secret management (SOPS, .env)
│   ├── release/        # Release history management
│   └── output/         # Structured output formatting
├── examples/           # Example configurations
└── test/               # Integration tests
```

## Making Changes

### Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Run `go vet` to catch common issues
- Keep functions focused and small
- Add comments for non-obvious logic

### Commit Messages

Use clear, descriptive commit messages:

```
Add --strict flag to validate command

- Fail on warnings when --strict is set
- Return proper exit code for CI pipelines
- Update help text with new flag
```

### Pull Requests

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes
4. Run tests (`go test ./...`)
5. Commit with clear messages
6. Push and create a Pull Request

### PR Checklist

- [ ] Tests pass locally
- [ ] Code follows project style
- [ ] New features have tests
- [ ] Documentation updated if needed
- [ ] Commit messages are clear

## Areas for Contribution

### Good First Issues

- Improve error messages
- Add more examples
- Documentation improvements
- Test coverage

### Feature Ideas

- Additional dependency types (elasticsearch, rabbitmq)
- Prometheus metrics export
- Custom resource support
- Namespace management

## Questions?

Open an issue for questions or discussion. We're happy to help!

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
