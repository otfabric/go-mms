// Package isostack orchestrates the full ISO client connection stack.
//
// It manages the layered sequence: TCP → COTP (via go-cotp) → Session →
// Presentation → ACSE, coordinating connection establishment, data
// exchange, and teardown across all layers.
//
// This package is internal — the public mms.Client delegates to it
// for all transport-level concerns.
package isostack
