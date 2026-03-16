# Contributing to dynamic-crf

Thank you for your interest in contributing.

## Prerequisites

- [Go](https://go.dev/dl/) 1.22+
- [FFmpeg](https://ffmpeg.org/download.html) with libvmaf support
- [MediaInfo](https://mediaarea.net/en/MediaInfo/Download) CLI
- [gofumpt](https://github.com/mvdan/gofumpt) for formatting
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2+

## Getting started

```bash
git clone https://github.com/terranvigil/dynamic-crf.git
cd dynamic-crf
make build
make test
```

## Development workflow

1. Fork the repository and create a feature branch from `main`.
2. Make your changes.
3. Ensure code compiles: `make build`
4. Format code: `make fmt`
5. Run linter: `make lint`
6. Run tests: `make test`
7. Commit with a clear message describing the change.
8. Open a pull request against `main`.

## Code style

- Format with `gofumpt` (not `gofmt`).
- Follow existing patterns in the codebase.
- Use `log/slog` for structured logging.
- Return errors instead of calling `log.Fatal` in library code.
- Use `context.Context` for cancellation in all operations that shell out to external tools.
- Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the error chain.

## Testing

- Unit tests use the `//go:build unit` build tag.
- Integration tests use `//go:build integration` and require media fixtures in `fixtures/media/`.
- Use table-driven tests where applicable.
- Run unit tests: `make test`
- Run integration tests: `go test -v -race -count=1 -tags=integration ./...`

## Reporting issues

Open a GitHub issue with:

- What you expected to happen
- What actually happened
- Steps to reproduce
- FFmpeg version, OS, and Go version
