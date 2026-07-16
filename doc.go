// SPDX-License-Identifier: MIT

// Package mms implements the Manufacturing Message Specification (MMS)
// protocol — ISO 9506.
//
// This package provides both client-side and server-side MMS
// implementations for communicating over the ISO/OSI stack
// (TPKT → COTP → Session → Presentation → ACSE → MMS).
//
// The library is designed as a generic MMS implementation. It does not
// contain any IEC 61850 domain logic — that belongs in a separate
// higher-level package built on top of go-mms.
//
// # Client quick start
//
// For real TCP connections, use the transport/iso subpackage:
//
//	import "github.com/otfabric/go-mms/transport/iso"
//
//	client, err := iso.Dial(ctx, "10.0.0.1:102",
//	    iso.WithCalledTSelector([]byte{0x00, 0x01}),
//	    iso.WithClientDialOptions(mms.DialOptions{
//	        MMS: mms.MMSOptions{MaxPDUSize: 65000},
//	    }),
//	)
//
// Or use [NewClient] with an already-established [Transport]:
//
//	client, err := mms.NewClient(ctx, conn, mms.DialOptions{...})
//
// Supported client services: [Client.Identify], [Client.Status],
// [Client.Read], [Client.ReadMultiple], [Client.Write],
// [Client.GetNameList], [Client.GetNameListAll],
// [Client.GetVariableAccessAttributes],
// [Client.DefineNamedVariableList],
// [Client.GetNamedVariableListAttributes],
// [Client.DeleteNamedVariableList],
// File services ([Client.FileOpen], [Client.FileRead],
// [Client.FileClose], [Client.FileDirectory], [Client.FileDelete],
// [Client.FileRename], [Client.ObtainFile]),
// Journal services ([Client.ReadJournalTimeRange],
// [Client.ReadJournalStartAfter]),
// and [Client.Close].
//
// Incoming InformationReport PDUs from the server are delivered via
// a registered callback:
//
//	client.OnInformationReport(func(r *mms.InformationReportIndication) {
//	    fmt.Println("report:", r.Values)
//	})
//
//	defer client.Close(ctx)
//
//	result, _ := client.Read(ctx, mms.ReadRequest{
//	    DomainID: "MyDomain",
//	    ItemID:   "MyVariable",
//	})
//	fmt.Println(result.Value.Type())
//
// # Server quick start
//
// Create a [Server] with [NewServer], register domains, variables, and
// handlers, then use [Server.ListenAndServe] with a
// [TransportListener] (or call [Server.Serve] per connection):
//
//	import "github.com/otfabric/go-mms/transport/iso"
//
//	srv := mms.NewServer(mms.ServerOptions{
//	    MMS: mms.ServerMMSOptions{MaxPDUSize: 65000},
//	})
//	srv.HandleIdentify(func(ctx context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
//	    return &mms.ServerIdentity{Vendor: "My Corp", Model: "Device", Revision: "1.0"}, nil
//	})
//	srv.RegisterDomain("process")
//	srv.RegisterVariable(mms.Variable{...})
//
//	ln, _ := iso.Listen(":102")
//	srv.ListenAndServe(ctx, ln) // owns ln; closes it on return
//
// Supported server services: Identify, Status, GetNameList,
// GetVariableAccessAttributes, Read, Write, DefineNamedVariableList,
// GetNamedVariableListAttributes, DeleteNamedVariableList.
//
// The server can send InformationReport to connected clients:
//
//	for _, sc := range srv.Connections() {
//	    sc.SendInformationReport(ctx, &mms.InformationReportRequest{
//	        Variables: []mms.ObjectName{{Scope: mms.ObjectScopeDomain, Domain: "dom", ItemID: "temp"}},
//	        Values:    []*mms.Value{mms.NewFloat(21.5)},
//	    })
//	}
//
// Or broadcast to all connected clients:
//
//	srv.Broadcast(ctx, &mms.InformationReportRequest{...})
//
// # Concurrency
//
// A [Client] is safe for concurrent use from multiple goroutines.
// Service calls are serialized internally via a send mutex — only
// one confirmed request send is in flight at a time per client.
// A background reader goroutine dispatches confirmed responses by
// invoke ID and delivers unconfirmed PDUs (InformationReport) to
// the registered handler.
//
// A [Server] is safe for concurrent use. Each [Server.Serve] call
// handles one association; multiple connections are served in parallel
// by calling Serve in separate goroutines. Within a single connection,
// confirmed requests are handled serially. Unconfirmed PDUs
// (InformationReport) can be sent concurrently via [ServerConn].
//
// # Supported services
//
// Client services:
//   - Identify, Status
//   - Read, ReadMultiple, Write
//   - GetNameList, GetNameListAll
//   - GetVariableAccessAttributes
//   - DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList
//   - InformationReport (receive via [Client.OnInformationReport])
//   - File services (Open/Read/Close/Directory/Delete/Rename/ObtainFile)
//   - Journal services (ReadJournal by time range or start-after)
//
// Server services:
//   - Identify, Status
//   - Read, Write
//   - GetNameList (Domain+VMD scope, NamedVariable+Domain/VMD scope, NamedVariableList+Domain/VMD scope)
//   - GetVariableAccessAttributes
//   - DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList
//   - InformationReport (send via [ServerConn.SendInformationReport] or [Server.Broadcast])
//   - File services (via [FileProvider])
//   - Journal services (via [JournalProvider])
//
// Named variable list services (DefineNamedVariableList,
// GetNamedVariableListAttributes, DeleteNamedVariableList) are
// supported on both client and server.
//
// # Transport
//
// The transport/iso subpackage provides real TCP+TPKT+COTP transport.
// It offers DialTCP (transport-only), Dial (transport + MMS client),
// and Listen (server-side accept with COTP handshake).
//
// For testing or custom transports, implement [Transport] directly
// and pass it to [NewClient] or [Server.Serve].
//
// Out of scope:
//   - IEC 61850 object-model or naming helpers
//
// # Server GetNameList combinations
//
// The server supports these ObjectClass+Scope combinations:
//   - ObjectClassDomain + ObjectScopeVMD → list domain names
//   - ObjectClassNamedVariable + ObjectScopeDomain → list variables in a domain
//   - ObjectClassNamedVariable + ObjectScopeVMD → list VMD-scoped variables
//   - ObjectClassNamedVariableList + ObjectScopeDomain → list NVLs in a domain
//   - ObjectClassNamedVariableList + ObjectScopeVMD → list VMD-scoped NVLs
//
// All other combinations return a service error.
//
// # Value ownership
//
// [Value] instances are safe to store and pass between goroutines.
// Constructors (e.g. [NewOctetString], [NewArray]) defensively copy
// byte slices and element slices so the caller retains no aliased
// references. Accessors that return byte slices (e.g. [Value.OctetString])
// likewise return copies. For composite values (arrays, structures), the
// element slice header is copied but child [*Value] pointers are shared;
// use [Value.Clone] when full deep-copy semantics are required.
//
// # Error handling
//
// Errors fall into several categories:
//
//   - Sentinel errors ([ErrClosed], [ErrConnectionRejected], etc.) can be
//     checked with [errors.Is].
//   - Typed errors ([ServiceError], [DataAccessError], [ProtocolError],
//     [AuthenticationError]) can be extracted with [errors.As] and carry
//     structured detail (error class, code, phase, etc.).
//   - Per-variable data access errors are returned as [*DataAccessError]
//     from single-variable helpers ([Client.Read], [Client.Write]) and as
//     [DataAccessErrorCode] fields in multi-variable results
//     ([AccessResult], [WriteAccessResult]).
//
// # Architecture
//
// The public API exposes MMS concepts only. All ISO stack internals
// (session, presentation, ACSE) are handled transparently. Transport
// is delegated to the transport/iso subpackage, which uses
// otfabric/go-tpkt and otfabric/go-cotp internally.
package mms
