package mms

import "context"

// Variable describes a named MMS variable registered with a [Server].
//
// At minimum, Name and TypeSpec must be set. Read is required for the
// variable to be readable; Write is required for it to be writable.
type Variable struct {
	Name     ObjectName
	TypeSpec TypeSpec

	// Deletable reports whether the variable can be deleted.
	Deletable bool

	// Read is called when a client reads this variable. If nil, reads
	// return a data-access-error (object-access-denied).
	Read func(ctx context.Context) (*Value, error)

	// Write is called when a client writes this variable. If nil, writes
	// return a data-access-error (object-access-denied).
	Write func(ctx context.Context, v *Value) error
}

// NamedVariableList is a pre-configured named variable list for server
// registration. Use [Server.RegisterNamedVariableList] to add static
// (non-deletable by default) NVLs to the server model.
type NamedVariableList struct {
	Name      ObjectName
	Deletable bool
	Variables []VariableSpec
}

// IdentifyRequest is the server-side representation of an incoming
// MMS Identify request. Currently empty (Identify has no parameters).
type IdentifyRequest struct{}

// StatusRequest is the server-side representation of an incoming
// MMS Status request.
type StatusRequest struct {
	ExtendedDerivation bool
}
