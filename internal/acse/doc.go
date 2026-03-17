// Package acse implements the Association Control Service Element
// (ISO 8650) for MMS association establishment and teardown.
//
// It handles construction and parsing of AARQ (associate request),
// AARE (associate response), ABRT (abort), RLRQ (release request),
// and RLRE (release response) PDUs.
//
// This package is internal — users of go-mms never interact with
// ACSE directly.
package acse
