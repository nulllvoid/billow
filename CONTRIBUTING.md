# Contributing to billow

Thank you for taking the time to contribute!

## How to contribute

### Reporting bugs
Open an issue using the **Bug report** template. Include a minimal reproducer and your Go / OS version.

### Requesting features
Open an issue using the **Feature request** template. Describe the problem you're solving, not just the solution you want.

### Submitting a pull request

1. **Fork** the repository and create a branch from `main`.
2. **Write tests** for your change — new behaviour must be covered.
3. Make sure **`go test -race ./...`** and **`go vet ./...`** pass locally.
4. Keep commits focused and atomic. Squash fixup commits before opening the PR.
5. Fill in the pull request template.

### Code style
- Follow standard Go idioms (`gofmt`, `go vet`, `golangci-lint`).
- Exported symbols must have a doc comment.
- Avoid external dependencies — billow intentionally has zero deps.
- Prefer clear, boring code over clever abstractions.

### Commit messages
Use the conventional format:

```
<type>: <short summary>

<optional body>
```

Types: `feat`, `fix`, `test`, `docs`, `refactor`, `ci`, `chore`.

## Development setup

```bash
git clone https://github.com/nulllvoid/billow
cd billow
go test -race ./...   # run all tests
go vet ./...          # static analysis
```

No external tools are required beyond a standard Go toolchain (≥ 1.21).

## Licence

By contributing you agree that your contributions will be licensed under the [MIT License](LICENSE).
