# AGENTS.md — go-mms

## What this is

Pure Go MMS (ISO 9506-1/2) client and server, the base layer of [go-iec61850](https://github.com/otfabric/go-iec61850), on [go-cotp](https://github.com/otfabric/go-cotp). Profile: **protocol-library**. Nothing is certified; independently verified behaviour and its peers are listed in INTEROP.md and COMPATIBILITY.md.

## Organisation standards

This repository follows **otfabric/engineering policy 1.2**, profile **protocol-library** (`.otfabric/policy.yaml`, mode `enforce`). Organisation skills: `otfabric` Claude Code plugin. Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Wire behaviour | ISO 9506-1/2; service coverage in COMPLIANCE.md |
| What is verified against independent stacks | INTEROP.md, COMPATIBILITY.md (iec61850-interop images pinned in `.github/workflows/interop.yml`) |
| Scope not implemented, known behaviour | KNOWN_LIMITATIONS.md, LIMITS.md |
| Goroutine ownership, timeouts, Close | OWNERSHIP.md, TIMEOUT_AND_CLOSE.md, RACE_NOTES.md |
| Errors | ERRORS.md |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`), RELEASE_CHECKLIST.md |
| Work in progress | PROGRESS.md |

## Hard rules

1. Never lower `COVER_PKG_MIN` (90, per package and in total, `scripts/coverage-gate.sh`), add lint suppressions, or skip/delete tests to make CI pass.
2. Never invent protocol semantics; cite the specification clause.
3. Interop claims only from passing runs against the pinned iec61850-interop images.
4. Exported API changes are breaking for go-iec61850: this library stays on v0 or v1, and a breaking change is listed in RELEASE.md.
5. No new runtime dependency beyond go-cotp without an ADR.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| a BER or PDU decoder | `make fuzz-ber`, `make fuzz-pdu` |
| concurrency or lifecycle | `make chaos` |
| interop-relevant behaviour | `make interop` or `make interop-all` (Docker; see INTEROP.md) |

## Definition of Done

`make check` green; tests for new behaviour including error paths; fuzz target for any new decoder; docs updated in the same change; RELEASE.md section for user-visible changes.

## Git and releases

Branches and PRs. Do not push to `main`, tag or run the Release workflow unless the maintainer asks in the session.
