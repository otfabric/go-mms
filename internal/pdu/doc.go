// SPDX-License-Identifier: MIT

// Package pdu handles MMS PDU construction and parsing.
//
// It defines the internal Go struct representations for MMS protocol
// data units (Initiate, ConfirmedRequest, ConfirmedResponse, Read,
// Write, etc.) and provides encode/decode functions for the wire format.
//
// This package depends on [internal/codec] and [internal/asn1util] for
// ASN.1 encoding details. Its types are not exposed in the public API.
package pdu
