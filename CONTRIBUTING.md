# Contributing to go-mms

## Development setup

- Go 1.23 or later (see [go.mod](go.mod)).
- [golangci-lint](https://golangci-lint.run/) and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for `make check`.
- Docker for the interop suite only.

```sh
git clone https://github.com/otfabric/go-mms.git
cd go-mms
make check
```

Run `make help` for the list of targets.

## Tests

- `make test`, `make test-race`: unit and end-to-end tests.
- `make coverage`: fails below `COVERAGE_MIN` (default 90), per package and in total. Do not lower it.
- `make fuzz`: every fuzz target for `FUZZTIME` (default 15s). `make fuzz-ber` and `make fuzz-pdu` run the decoder targets only.
- `make chaos`: chaos and stress tests under the race detector.
- `make interop`, `make interop-all`: tests against independent stacks, see [INTEROP.md](INTEROP.md).

Add a fuzz target for any new decoder of untrusted input.

## Submitting changes

1. Work on a branch and open a pull request against `main`.
2. Add tests, including error paths.
3. Update the documents the change touches ([API.md](API.md), [ERRORS.md](ERRORS.md), [COMPLIANCE.md](COMPLIANCE.md), [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)) and add a section to [RELEASE.md](RELEASE.md) for user-visible changes.

Report security issues as described in [SECURITY.md](SECURITY.md), not in public issues.
