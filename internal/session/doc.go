// Package session implements the ISO 8327 session layer protocol.
//
// It handles construction and parsing of session SPDUs: CONNECT,
// ACCEPT, DATA, FINISH, DISCONNECT, ABORT, and REFUSE.
//
// This package is internal — users of go-mms never interact with
// the session layer directly.
package session
