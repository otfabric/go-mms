package mms

import (
	"context"
	"log/slog"
	"sync"
)

// Client is an MMS client connection to a remote MMS server.
//
// A Client is created via [Dial] and must be closed with [Client.Close]
// when no longer needed. It is safe to call methods concurrently from
// multiple goroutines.
type Client struct {
	mu     sync.Mutex
	closed bool
	logger *slog.Logger
	opts   DialOptions
}

// Dial establishes an MMS connection to the server at addr.
//
// The address should be in "host:port" format (e.g., "10.0.0.1:102").
// The connection performs the full ISO stack handshake (COTP → Session →
// Presentation → ACSE) followed by MMS Initiate negotiation.
//
// The provided context controls the connection timeout. If the context
// is cancelled before the connection is established, Dial returns the
// context error.
func Dial(ctx context.Context, addr string, opts DialOptions) (*Client, error) {
	_ = ctx
	_ = addr
	return nil, ErrUnsupported
}

// Close performs a clean MMS conclude and tears down the connection.
// After Close returns, no further operations may be performed on the client.
func (c *Client) Close(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.closed = true
	return ErrUnsupported
}

// Identify sends an MMS Identify request and returns the server's identity.
func (c *Client) Identify(ctx context.Context) (*ServerIdentity, error) {
	_ = ctx
	return nil, ErrUnsupported
}

// Status sends an MMS Status request and returns the VMD status.
func (c *Client) Status(ctx context.Context) (*ServerStatus, error) {
	_ = ctx
	return nil, ErrUnsupported
}

// ReadRequest specifies what to read from the MMS server.
type ReadRequest struct {
	DomainID DomainID
	ItemID   ItemID
}

// ReadResult holds the result of an MMS Read operation.
type ReadResult struct {
	Value *Value
}

// Read sends an MMS Read request for a single named variable.
func (c *Client) Read(ctx context.Context, req ReadRequest) (*ReadResult, error) {
	_ = ctx
	_ = req
	return nil, ErrUnsupported
}

// WriteRequest specifies what to write to the MMS server.
type WriteRequest struct {
	DomainID DomainID
	ItemID   ItemID
	Value    *Value
}

// WriteResult holds the result of an MMS Write operation.
type WriteResult struct{}

// Write sends an MMS Write request for a single named variable.
func (c *Client) Write(ctx context.Context, req WriteRequest) (*WriteResult, error) {
	_ = ctx
	_ = req
	return nil, ErrUnsupported
}

// NameListRequest specifies the scope and filter for a GetNameList operation.
type NameListRequest struct {
	ObjectClass ObjectClass
	DomainID    DomainID // empty for VMD-specific scope
}

// NameListResult holds the result of a GetNameList operation.
type NameListResult struct {
	Names        []string
	MoreFollows  bool
	Continuation string
}

// GetNameList retrieves a list of named objects from the server.
func (c *Client) GetNameList(ctx context.Context, req NameListRequest) (*NameListResult, error) {
	_ = ctx
	_ = req
	return nil, ErrUnsupported
}
