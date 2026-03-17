// Package codec provides MMS-specific marshal/unmarshal wrappers built
// on top of [encoding/asn1].
//
// This package is the primary interface between MMS protocol logic and
// ASN.1 encoding. It uses the stdlib as the default implementation tool,
// adding only thin helpers for patterns that encoding/asn1 cannot express
// directly (e.g., CHOICE dispatch).
//
// This package is internal — its types and functions are not part of the
// public API contract.
package codec
