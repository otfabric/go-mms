// Package asn1util provides thin helpers for gaps in [encoding/asn1].
//
// This package must remain minimal. It exists only for MMS wire
// requirements that cannot be expressed cleanly through the stdlib
// ASN.1 API (e.g., raw tag inspection, specific tag constants).
//
// If this package starts growing into generic BER/TLV infrastructure,
// stop and reassess the codec strategy.
//
// This package is internal — its types and functions are not part of the
// public API contract.
package asn1util
