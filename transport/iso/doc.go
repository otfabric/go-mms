// SPDX-License-Identifier: MIT

// Package iso provides TCP+TP0 transport integration for go-mms.
//
// This package bridges the gap between raw TCP connections and the
// mms.Transport interface used by the MMS client and server. It handles:
//
//   - TCP connection establishment and acceptance
//   - Optional TLS wrapping
//   - COTP class 0 (TP0) via otfabric/go-cotp (Connect/Accept, TSDU I/O)
//   - TSAP selector configuration
//
// TPKT framing and the COTP handshake are owned by go-cotp; this package
// maps [mms.Transport] Send/Receive onto WriteTSDU/ReadTSDU.
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
// # Addressing
//
// A client sends a calling and called T-selector of 00 01 and proposes a
// 1024-octet TPDU, and go-mms adds S-selector 00 01, P-selector 00 00 00 01
// and AP-titles 1.1.1.999 / 1.1.1.999.1 with AE-qualifier 12 (see
// [mms.ISOOptions]): the addressing libiec61850 uses and IEDs expect. Use
// [WithCallingTSelector], [WithCalledTSelector] and [WithMaxTPDULength] for
// an IED configured otherwise (the OSI-TSEL of its SCL ConnectedAP):
//
//	conn, err := iso.Dial(ctx, "10.0.0.1:102",
//	    iso.WithCallingTSelector([]byte{0x00, 0x01}),
//	    iso.WithCalledTSelector([]byte{0x00, 0x00, 0x00, 0x01}),
//	)
//
// A [Listener] accepts any called selector and returns the CR's selectors
// in the CC. [WithCalledTSelector] makes it strict: a CR whose called
// selector is absent or different is refused with a Disconnect Request
// (reason 3, address unknown).
//
// # Architecture
//
// The layering is: TCP → [TLS] → TPKT → COTP → Session → Presentation → ACSE → MMS.
// This package owns TCP (+ optional TLS) and adapts go-cotp's TP0 Conn to
// [mms.Transport]. go-mms core owns the upper four layers.
package iso
