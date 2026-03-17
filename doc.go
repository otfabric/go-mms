// Package mms implements the Manufacturing Message Specification (MMS)
// protocol — ISO 9506.
//
// This package provides a client-side MMS implementation for communicating
// with MMS servers over the ISO/OSI stack (TPKT → COTP → Session →
// Presentation → ACSE → MMS).
//
// The library is designed as a generic MMS implementation. It does not
// contain any IEC 61850 domain logic — that belongs in a separate
// higher-level package built on top of go-mms.
//
// # Quick start
//
//	client, err := mms.Dial(ctx, "10.0.0.1:102", mms.DialOptions{
//	    Transport: mms.TransportOptions{
//	        LocalTSelector:  []byte{0x00, 0x01},
//	        RemoteTSelector: []byte{0x00, 0x01},
//	    },
//	    ISO: mms.ISOOptions{
//	        LocalAPTitle:  mms.APTitle{1, 1, 1, 1},
//	        RemoteAPTitle: mms.APTitle{1, 1, 1, 1},
//	    },
//	    MMS: mms.MMSOptions{
//	        MaxPDUSize: 65000,
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close(ctx)
//
// # Architecture
//
// The public API exposes MMS concepts only. All ISO stack internals
// (session, presentation, ACSE) are handled transparently. Transport
// is delegated to otfabric/go-tpkt and otfabric/go-cotp.
package mms
