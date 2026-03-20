# go-mms Releases

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
