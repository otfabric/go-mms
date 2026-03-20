# go-mms Releases

## v0.1.3

**Fixed**: Race condition in `Client.Close` / conclude handshake.

When the reader loop received a `ConcludeResponse`, it signaled `concludeCh` and then returned (closing `readerDone`). Because Go's `select` picks randomly among ready cases, `conclude()` could non-deterministically hit the `readerDone` case and return a spurious `"connection closed before conclude response"` error. The fix drains `concludeCh` when `readerDone` fires before reporting failure.

**Changed**: Lint configuration and code quality.

- Restored sensible test-file exclusions in `.golangci.yml` for `errcheck` and `staticcheck` SA2002.
- Fixed `godot` comment punctuation in `internal/acse` and `internal/presentation`.
- Fixed `exhaustive` switch in `internal/acse` (missing `default` case for `AuthMechanism`).
- Fixed `errcheck` in `transport/iso` (unchecked `SetDeadline` / `SetWriteDeadline` / `SetReadDeadline` return values).
- Fixed `staticcheck` ST1023 redundant type declarations in test files.
- `make check` now passes cleanly.

---

## v0.1.2

**Changed**: Lowered minimum required Go version to 1.21 (was 1.22). All documentation, CI, and go.mod references updated accordingly. No code changes.

This release ensures compatibility with Go 1.21. No new features or bugfixes are included.

---

## v0.1.1

**Changed**: Add UTCTime Quality Byte Support — completed across all 4 layers.

- PDU layer (internal/pdu/data.go): Added TimeQuality uint8 to DataValue. Updated encodeUTCTime to write the quality byte and decodeUTCTime to return it (previously hardcoded 0x00 / discarded).
- Public Value type (value.go): Added timeQuality field, UTCTimeQuality() accessor, and NewUTCTimeWithQuality(t, quality) constructor. Updated NewUTCTime(t) to default to 0x0a (matching C reference). Updated Clone, Equal, and String to handle quality.
- Wire conversion (mms.go): Updated valueToDataValue and dataValueToValue to thread the quality byte between public and internal representations.
- Public constants (value.go): Added 5 named constants — UTCTimeQualityLeapSecondsKnown (0x80), UTCTimeQualityClockFailure (0x40), UTCTimeQualityClockNotSynchronized (0x20), UTCTimeQualityAccuracyMask (0x1F), UTCTimeQualityAccuracyUnspecified (0x1F).

---

## v0.1.0

**Changed**: N/A

Initial release.

---
