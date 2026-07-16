// SPDX-License-Identifier: MIT

// Package asn1util provides thin helpers for gaps in [encoding/asn1].
//
// This package must remain minimal. It exists only for MMS wire
// requirements that cannot be expressed cleanly through the stdlib
// ASN.1 API (e.g., raw tag inspection, specific tag constants).
//
// If this package starts growing into generic BER/TLV infrastructure,
// stop and reassess the codec strategy.
//
// # Tag number limitation
//
// All helpers in this package (WrapConstructed, WrapPrimitive,
// TagNumber, tag constants) assume the single-byte BER tag form,
// which supports tag numbers 0–30. Tag number 31 and above require
// multi-byte encoding, which is not implemented. This is sufficient
// for all known MMS ASN.1 tag values.
//
// This package is internal — its types and functions are not part of the
// public API contract.
package asn1util
