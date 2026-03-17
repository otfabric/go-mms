// Package transport defines the interface between the MMS client and
// the underlying ISO transport stack (TPKT + COTP).
//
// The Transport interface represents a connected COTP session. Data
// passed through Send/Receive is the payload above COTP — typically
// session SPDUs containing presentation and MMS data.
//
// Production implementations will use otfabric/go-tpkt and
// otfabric/go-cotp. Test code uses mock transports.
//
// This package is internal — users interact with mms.Client.
package transport

import "context"

// Transport is an established COTP connection that can send and
// receive session-layer data.
type Transport interface {
	// Send transmits data to the remote peer. The data is the raw
	// session SPDU bytes to be carried as COTP user data.
	Send(ctx context.Context, data []byte) error

	// Receive blocks until data is available from the remote peer
	// or the context is cancelled. Returns the session SPDU bytes
	// received as COTP user data.
	Receive(ctx context.Context) ([]byte, error)

	// Close closes the transport connection.
	Close() error
}
