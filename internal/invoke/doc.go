// Package invoke manages MMS invoke ID allocation and request/response
// correlation.
//
// It tracks outstanding confirmed requests, matches incoming responses
// to their originating requests by invoke ID, and enforces bounded
// lifetimes for outstanding calls.
//
// This package is internal — invoke ID management is transparent to
// users of the public mms.Client API.
package invoke
