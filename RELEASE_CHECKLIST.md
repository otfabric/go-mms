# Release Checklist

Steps for every go-mms release. RELEASE.md holds the notes; the
**Release** workflow (`.github/workflows/release.yml`, `workflow_dispatch`
with a `patch` / `minor` / `major` bump) tags the version and publishes
the GitHub release from the top section of RELEASE.md (up to the first
`---` rule).

## Before the PR

- [ ] `make check` is green: fmt, tidy, vet, golangci-lint, govulncheck,
      tests, race detector, per-package coverage gate (≥90% per package
      and in total, `scripts/coverage-gate.sh`), 2s fuzz pass over every
      target, armv7 compile.
- [ ] `make interop` is green against the libiec61850 image pinned in
      `.github/workflows/interop.yml`. When mms-interop publishes a new
      adapter, bump that digest and `interop/testdata/interop.json`.
- [ ] `TestAPICoverage` (e2e) and `TestInteropCoverage` (interop) list
      every new exported method.
- [ ] For changes to decoders, transport or lifecycle: `make fuzz`
      (15s per target or longer) and `make chaos` (30s, random seed).
- [ ] New public API is additive for a minor release; nothing is removed
      or changed incompatibly outside a major release (COMPATIBILITY.md).

## Docs

- [ ] RELEASE.md: new section at the top (date, previous release,
      summary, Added / Changed / Fixed / Tests / Docs), ending with `---`.
- [ ] API.md, ERRORS.md, OBSERVABILITY.md, TIMEOUT_AND_CLOSE.md, LIMITS.md,
      KNOWN_LIMITATIONS.md, COMPLIANCE.md and INTEROP.md match the code.
- [ ] README.md examples still compile against the API.

## Release

- [ ] PR CI (CI, quality gates, Interop) green; PR merged to `main`.
- [ ] Run the **Release** workflow on `main` with the bump type.
- [ ] Check the tag and the GitHub release notes.
- [ ] Tell downstream modules (go-iec61850, iec61850ctl) about new API
      and behaviour changes.
