// Package presentation implements the ISO 8823 presentation layer protocol.
//
// It handles construction and parsing of presentation PDUs: CP-type
// (connect), CPA-type (connect accept), user data, and abstract syntax
// context negotiation for ACSE and MMS.
//
// This package is internal — users of go-mms never interact with
// the presentation layer directly.
package presentation
