// Package iso provides TCP+TPKT+COTP transport integration for go-mms.
//
// This package bridges the gap between raw TCP connections and the
// mms.Transport interface used by the MMS client and server. It handles:
//
//   - TCP connection establishment and acceptance
//   - TPKT framing (RFC 1006) via otfabric/go-tpkt
//   - COTP (X.224 class 0) handshake via otfabric/go-cotp
//   - TSAP selector configuration
//
// # Client usage
//
//	conn, err := iso.DialTCP(ctx, "10.0.0.1:102",
//	    iso.WithCalledTSelector([]byte{0x00, 0x01}),
//	)
//	if err != nil { ... }
//	client, err := mms.NewClient(ctx, conn, mms.DialOptions{...})
//
// Or use the convenience function:
//
//	client, err := iso.Dial(ctx, "10.0.0.1:102",
//	    iso.WithCalledTSelector([]byte{0x00, 0x01}),
//	    iso.WithClientDialOptions(mms.DialOptions{...}),
//	)
//
// # Server usage
//
//	ln, err := iso.Listen(":102")
//	if err != nil { ... }
//	err = server.ListenAndServe(ctx, ln) // owns ln; closes it on return
//
// # TLS
//
// Use [WithTLSConfig] to enable TLS transport security. When TLS is
// configured, the connection layering becomes:
//
//	TCP → TLS → TPKT → COTP → Session → Presentation → ACSE → MMS
//
// The standard port for MMS over TLS is 3782 (vs 102 for plaintext).
// Plaintext and TLS listeners can coexist on the same server.
//
// Peer certificates from TLS connections are accessible via the
// [mms.TLSTransport] interface for server-side authentication policy.
//
// # Security note on ACSE password authentication
//
// ACSE password authentication ([mms.ISOOptions].Password) embeds
// credentials inside the association request PDU. Without TLS, the
// password travels in the clear as part of the TPKT/COTP payload —
// this is plaintext-equivalent on port 102. ACSE password
// authentication should be combined with TLS transport to provide
// confidentiality. The library does not enforce this combination.
//
// # TSEL configuration
//
// Use [WithCallingTSelector] and [WithCalledTSelector] to configure
// COTP Transport Service Access Point (TSAP) selectors:
//
//	conn, err := iso.Dial(ctx, "10.0.0.1:102",
//	    iso.WithCallingTSelector([]byte{0x00, 0x01}),
//	    iso.WithCalledTSelector([]byte{0x00, 0x01}),
//	)
//
// On the server side, [WithCalledTSelector] validates that incoming
// COTP Connection Requests carry the expected called selector; mismatches
// are rejected with a Disconnect Request.
//
// # Architecture
//
// The layering is: TCP → [TLS] → TPKT → COTP → Session → Presentation → ACSE → MMS.
// This package owns the first three layers (plus optional TLS). go-mms
// core owns the upper four.
package iso
