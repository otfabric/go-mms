# Public API Review

Comprehensive inventory of every exported symbol in the `go-mms` public API.
Each symbol is classified for release readiness.

**Classification key:**

| Label | Meaning |
|-------|---------|
| **stable keep** | Well-named, well-designed, ready for 1.0 |
| **keep but rename** | Functionality is right but name could be better |
| **keep but document sharper** | Needs better docs but API is fine |
| **move internal** | Should not be public |
| **deprecate** | Remove before first tagged release |

---

## Root Package (`github.com/otfabric/go-mms`)

### Client

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `Client` | struct | stable keep | Core client type. Unexported fields, clean concurrency model. |
| `NewClient(ctx, conn, opts)` | func | stable keep | Low-level constructor for custom transports. |
| `Client.Close(ctx)` | method | stable keep | Orderly shutdown with conclude handshake. Idempotent. |
| `Client.Abort(ctx)` | method | stable keep | Immediate abort without conclude. Good emergency API. |
| `Client.Identify(ctx)` | method | stable keep | Clean, single-purpose. |
| `Client.Status(ctx)` | method | stable keep | Simple default-parameters convenience. |
| `Client.StatusWithOptions(ctx, req)` | method | stable keep | Explicit options variant follows Go convention. |
| `Client.Negotiated()` | method | stable keep | Read-only accessor for negotiated params. |
| `Client.OnInformationReport(handler)` | method | stable keep | Callback registration, nil-safe unregister. |
| `Client.Read(ctx, req)` | method | stable keep | Single-variable convenience using DomainID/ItemID. |
| `Client.ReadMultiple(ctx, vars)` | method | stable keep | Multi-variable read by ObjectName. Core primitive. |
| `Client.ReadObject(ctx, name)` | method | keep but document sharper | Thin wrapper over ReadMultiple. Document when to use this vs Read. |
| `Client.ReadVariables(ctx, vars)` | method | stable keep | Full-featured read with alternate access. |
| `Client.ReadComponent(ctx, name, component)` | method | stable keep | Convenient shorthand for component-level read. |
| `Client.ReadByIndex(ctx, name, index)` | method | keep but document sharper | Semantically identical to ReadArrayElement for arrays. Clarify struct-vs-array dual use. |
| `Client.ReadArrayElement(ctx, name, index)` | method | keep but document sharper | Same wire encoding as ReadByIndex. Document the overlap or deprecate one. |
| `Client.ReadArrayRange(ctx, name, start, count)` | method | stable keep | Clear purpose, no overlap. |
| `Client.ReadNamedVariableList(ctx, name, opts...)` | method | stable keep | NVL read with optional spec-with-result. |
| `Client.Write(ctx, req)` | method | stable keep | Single-variable convenience. |
| `Client.WriteObject(ctx, name, value)` | method | stable keep | ObjectName-based write for all scopes. |
| `Client.WriteVariables(ctx, vars, values)` | method | stable keep | Multi-variable write with alternate access. |
| `Client.WriteComponent(ctx, name, comp, val)` | method | stable keep | Component-level write shorthand. |
| `Client.WriteArrayElement(ctx, name, idx, val)` | method | stable keep | Element-level array write. |
| `Client.WriteNamedVariableList(ctx, name, values)` | method | stable keep | NVL write with partial-success semantics. |
| `Client.GetNameList(ctx, req)` | method | stable keep | Paginated name listing. |
| `Client.GetNameListAll(ctx, req)` | method | stable keep | Auto-paging convenience. Stall detection is a nice touch. |
| `Client.GetVariableAccessAttributes(ctx, name)` | method | stable keep | Type introspection service. |
| `Client.DefineNamedVariableList(ctx, req)` | method | stable keep | NVL creation. |
| `Client.GetNamedVariableListAttributes(ctx, name)` | method | stable keep | NVL attribute retrieval. |
| `Client.DeleteNamedVariableList(ctx, names)` | method | stable keep | NVL deletion. |
| `Client.FileOpen(ctx, name, opts...)` | method | stable keep | File open with optional initial position. |
| `Client.FileRead(ctx, frsmID)` | method | stable keep | Chunked file read. |
| `Client.FileReadAll(ctx, frsmID)` | method | stable keep | Convenience: reads all chunks. |
| `Client.FileClose(ctx, frsmID)` | method | stable keep | Explicit close. |
| `Client.FileDelete(ctx, name)` | method | stable keep | File deletion. |
| `Client.FileRename(ctx, cur, new)` | method | stable keep | File rename. |
| `Client.ObtainFile(ctx, src, dst)` | method | stable keep | Server-side file copy. |
| `Client.FileDirectory(ctx, req)` | method | stable keep | Paginated directory listing. |
| `Client.FileDirectoryAll(ctx, spec)` | method | stable keep | Auto-paging directory listing with stall detection. |
| `Client.DownloadFile(ctx, name)` | method | stable keep | Open+ReadAll+Close convenience. |
| `Client.ReadJournalTimeRange(ctx, domain, journal, start, stop)` | method | stable keep | Journal read by time window. |
| `Client.ReadJournalStartAfter(ctx, domain, journal, time, id)` | method | stable keep | Journal pagination. |

### Client Request/Result Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ReadRequest` | struct | stable keep | Simple DomainID + ItemID pair. |
| `ReadResult` | struct | stable keep | Wraps `*Value`. |
| `AccessResult` | struct | stable keep | Per-variable result with error code. |
| `WriteRequest` | struct | stable keep | DomainID + ItemID + Value. |
| `WriteResult` | struct | stable keep | Empty struct — marker for success. |
| `WriteAccessResult` | struct | stable keep | Per-variable write outcome. |
| `ClientStatusRequest` | struct | keep but rename | Consider `StatusOptions` for consistency with other option types. |
| `NameListRequest` | struct | stable keep | Clean scope/class/continuation. |
| `NameListResult` | struct | stable keep | Names + MoreFollows. |
| `DefineNamedVariableListRequest` | struct | stable keep | ListName + Variables. |
| `NamedVariableListAttributes` | struct | stable keep | Deletable + Variables result. |
| `DeleteNamedVariableListResult` | struct | stable keep | Matched + Deleted counts. |
| `VariableAccessAttributes` | struct | stable keep | Deletable + TypeSpec result. |
| `ReadNamedVariableListOptions` | struct | stable keep | SpecificationWithResult flag. Zero-value safe. |
| `NVLAccessResult` | struct | keep but document sharper | Not currently returned by any public method. Document intended use or remove. |
| `FileOpenOptions` | struct | stable keep | Zero-value safe (position 0). |
| `FileOpenResult` | struct | stable keep | FrsmID + Size + LastModified. |
| `FileReadResult` | struct | stable keep | Data + MoreFollows. |
| `FileDirectoryRequest` | struct | stable keep | FileSpec + ContinueAfter. |
| `FileDirectoryResult` | struct | stable keep | Entries + MoreFollows + ContinueAfter. |
| `FileDirectoryEntry` | struct | stable keep | Name + Size + LastModified. |
| `NegotiatedParameters` | struct | stable keep | Read-only negotiated params. |

### Server

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `Server` | struct | stable keep | Core server type. |
| `NewServer(opts)` | func | stable keep | Constructor with options. |
| `Server.HandleIdentify(h)` | method | stable keep | Handler registration. |
| `Server.HandleStatus(h)` | method | stable keep | Handler registration. |
| `Server.RegisterDomain(name)` | method | stable keep | Domain registration. |
| `Server.RegisterVariable(v)` | method | stable keep | Variable registration with validation. |
| `Server.RegisterNamedVariableList(nvl)` | method | stable keep | Static NVL registration. |
| `Server.Serve(ctx, conn)` | method | stable keep | Single-connection serve loop. |
| `Server.ListenAndServe(ctx, ln)` | method | stable keep | Accept loop. Owns the listener. |
| `Server.Connections()` | method | stable keep | Snapshot of active connections. |
| `Server.Broadcast(ctx, req)` | method | stable keep | InformationReport to all clients. |
| `ServerConn` | struct | stable keep | Per-connection handle for unconfirmed PDUs. |
| `ServerConn.SendInformationReport(ctx, req)` | method | stable keep | Send report to one client. |
| `ServerConn.AuthToken()` | method | stable keep | Opaque security token retrieval. |

### Server Configuration Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ServerOptions` | struct | stable keep | MMS + Logger + Authenticate + FileProvider + JournalProvider. |
| `ServerMMSOptions` | struct | stable keep | MaxPDUSize, MaxOutstanding, NestingLevel. Zero-value safe. |
| `Variable` | struct | stable keep | Server-side variable definition. |
| `NamedVariableList` | struct | stable keep | Server-side NVL definition. |
| `IdentifyRequest` | struct | stable keep | Empty — Identify has no parameters. |
| `StatusRequest` | struct | stable keep | ExtendedDerivation flag. |

### Client Configuration Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `DialOptions` | struct | stable keep | Transport + ISO + MMS + Logger + RawHook. |
| `TransportOptions` | struct | stable keep | LocalTSelector + RemoteTSelector. |
| `ISOOptions` | struct | stable keep | AP titles, AE qualifiers, P/S selectors, password. |
| `MMSOptions` | struct | stable keep | MaxPDUSize, MaxOutstanding, NestingLevel. Zero-value safe. |

### Interfaces

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `Transport` | interface | stable keep | Core transport abstraction: Send/Receive/Close. |
| `TransportListener` | interface | stable keep | Accept/Close/Addr for server-side transport. |
| `TLSTransport` | interface | stable keep | Optional TLS state accessor. Type-assertion pattern. |
| `RemoteAddrTransport` | interface | stable keep | Optional remote address accessor. Type-assertion pattern. |
| `FileProvider` | interface | stable keep | Server-side file service abstraction. Complete: List/Open/Read/Close/Delete/Rename/ObtainFile. |
| `JournalProvider` | interface | stable keep | Server-side journal service abstraction. ListJournals/ReadTimeRange/ReadStartAfter. |
| `Authenticator` | func type | stable keep | Authentication callback for server. Well-documented with examples. |

### Core Data Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `Value` | struct | stable keep | Core data type. Opaque fields, accessor pattern. |
| `Value.Type()` | method | stable keep | Type discriminator. |
| `Value.Bool()` | method | stable keep | `(bool, bool)` accessor. |
| `Value.Int32()` | method | stable keep | `(int32, bool)` with overflow check. |
| `Value.Int64()` | method | stable keep | `(int64, bool)` accessor. |
| `Value.Uint32()` | method | stable keep | `(uint32, bool)` with overflow check. |
| `Value.Uint64()` | method | stable keep | `(uint64, bool)` accessor. |
| `Value.Float32()` | method | stable keep | `(float32, bool)` accessor. |
| `Value.Float64()` | method | stable keep | `(float64, bool)` accessor. |
| `Value.BitString()` | method | stable keep | Returns copy. |
| `Value.BitStringLength()` | method | stable keep | Bit count accessor. |
| `Value.OctetString()` | method | stable keep | Returns copy. |
| `Value.VisibleString()` | method | stable keep | String accessor. |
| `Value.MmsString()` | method | stable keep | MMS string accessor. |
| `Value.UTCTime()` | method | stable keep | Time accessor. |
| `Value.BinaryTime()` | method | stable keep | Milliseconds accessor. |
| `Value.GeneralizedTime()` | method | stable keep | Time accessor. |
| `Value.BCD()` | method | stable keep | BCD integer accessor. |
| `Value.ObjectIdentifier()` | method | stable keep | OID arcs accessor, returns copy. |
| `Value.Structure()` | method | stable keep | Shallow-copy element slice. |
| `Value.ArrayElements()` | method | stable keep | Shallow-copy element slice. |
| `Value.DataAccessErr()` | method | stable keep | Error code accessor. |
| `Value.Clone()` | method | stable keep | Deep copy. |
| `Value.Equal(other)` | method | stable keep | Deep equality. |
| `Value.String()` | method | stable keep | Human-readable for debugging. |
| `Value.Get(selectors...)` | method | keep but document sharper | Component access returns error about needing TypeSpec context. Document this limitation prominently. |

### Value Constructors

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `NewBoolean(b)` | func | stable keep | |
| `NewInteger(i)` | func | stable keep | |
| `NewUnsigned(u)` | func | stable keep | |
| `NewFloat(f)` | func | stable keep | |
| `NewBitString(bits)` | func | stable keep | All bits valid (bitLen = len*8). |
| `NewBitStringWithLength(bits, bitLen)` | func | stable keep | Explicit bit length. |
| `NewOctetString(data)` | func | stable keep | |
| `NewVisibleString(s)` | func | stable keep | |
| `NewMmsString(s)` | func | stable keep | |
| `NewUTCTime(t)` | func | stable keep | |
| `NewBinaryTime(ms)` | func | stable keep | |
| `NewGeneralizedTime(t)` | func | stable keep | |
| `NewBCD(v)` | func | stable keep | |
| `NewObjectIdentifier(oid)` | func | stable keep | Defensive copy. |
| `NewArray(elements)` | func | stable keep | Shallow copy of slice. |
| `NewStructure(elements)` | func | stable keep | Shallow copy of slice. |
| `NewDataAccessError(code)` | func | stable keep | |

### Type System

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `TypeSpec` | struct | stable keep | Recursive type descriptor. Supports all MMS types. |
| `TypeSpecElement` | struct | stable keep | Named element within a structure TypeSpec. |
| `TypeSpec.ChildByName(name)` | method | stable keep | Structure child lookup. |
| `TypeSpec.ChildByIndex(index)` | method | stable keep | Structure/array child lookup. |
| `TypeSpec.ShallowCompatible(v)` | method | keep but document sharper | Name is non-obvious. Document what "shallow" means precisely (top-level type + element count). |
| `TypeSpec.DefaultValue()` | method | stable keep | Zero-value factory. Handles recursive types. |
| `TypeSpec.Resolve(selectors...)` | method | stable keep | Walks type tree by selectors. |

### Named Types and Enums

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `DomainID` | type (string) | stable keep | Self-documenting named string. |
| `ItemID` | type (string) | stable keep | Self-documenting named string. |
| `InvokeID` | type (uint32) | stable keep | Protocol correlation ID. |
| `APTitle` | type alias (asn1.ObjectIdentifier) | stable keep | Direct asn1 compatibility via alias. |
| `ValueType` | type (int) | stable keep | Enum for value types. |
| `ObjectClass` | type (int) | stable keep | Enum for MMS object classes. |
| `ObjectScope` | type (int) | stable keep | Enum for naming scopes. |
| `DataAccessErrorCode` | type (int) | stable keep | Per-variable access error enum. |
| `ErrorClass` | type (int) | stable keep | ConfirmedError class enum. |
| `VMDLogicalStatus` | type (int) | stable keep | Logical status enum. |
| `VMDPhysicalStatus` | type (int) | stable keep | Physical status enum. |
| `AuthMechanism` | type (int) | stable keep | Authentication mechanism enum. |

### Enum Constants — ValueType

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ValueTypeBoolean` | const | stable keep | |
| `ValueTypeInteger` | const | stable keep | |
| `ValueTypeUnsigned` | const | stable keep | |
| `ValueTypeFloat` | const | stable keep | |
| `ValueTypeBitString` | const | stable keep | |
| `ValueTypeOctetString` | const | stable keep | |
| `ValueTypeVisibleString` | const | stable keep | |
| `ValueTypeMmsString` | const | stable keep | |
| `ValueTypeUTCTime` | const | stable keep | |
| `ValueTypeBinaryTime` | const | stable keep | |
| `ValueTypeArray` | const | stable keep | |
| `ValueTypeStructure` | const | stable keep | |
| `ValueTypeDataAccessError` | const | stable keep | |
| `ValueTypeNamedType` | const | stable keep | |
| `ValueTypeGeneralizedTime` | const | stable keep | |
| `ValueTypeBCD` | const | stable keep | |
| `ValueTypeObjectIdentifier` | const | stable keep | |

### Enum Constants — ObjectClass

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ObjectClassNamedVariable` | const | stable keep | |
| `ObjectClassScatteredAccess` | const | stable keep | |
| `ObjectClassNamedVariableList` | const | stable keep | |
| `ObjectClassNamedType` | const | stable keep | |
| `ObjectClassSemaphore` | const | stable keep | |
| `ObjectClassEventCondition` | const | stable keep | |
| `ObjectClassEventAction` | const | stable keep | |
| `ObjectClassEventEnrollment` | const | stable keep | |
| `ObjectClassJournal` | const | stable keep | |
| `ObjectClassDomain` | const | stable keep | |
| `ObjectClassProgramInvocation` | const | stable keep | |
| `ObjectClassOperatorStation` | const | stable keep | |

### Enum Constants — ObjectScope

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ObjectScopeVMD` | const | stable keep | Zero value = VMD (the common default). |
| `ObjectScopeDomain` | const | stable keep | |
| `ObjectScopeAssociation` | const | stable keep | |

### Enum Constants — DataAccessErrorCode

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `DataAccessErrorNone` | const | stable keep | |
| `DataAccessErrorObjectInvalidated` | const | stable keep | |
| `DataAccessErrorHardwareFault` | const | stable keep | |
| `DataAccessErrorTemporarilyUnavail` | const | keep but rename | Consider `DataAccessErrorTemporarilyUnavailable` (unabbreviated). |
| `DataAccessErrorObjectAccessDenied` | const | stable keep | |
| `DataAccessErrorObjectUndefined` | const | stable keep | |
| `DataAccessErrorInvalidAddress` | const | stable keep | |
| `DataAccessErrorTypeMismatch` | const | stable keep | |
| `DataAccessErrorTypeInconsistent` | const | stable keep | |
| `DataAccessErrorObjectExists` | const | stable keep | |
| `DataAccessErrorObjectAccessUnsup` | const | keep but rename | Consider `DataAccessErrorObjectAccessUnsupported` (unabbreviated). |

### Enum Constants — ErrorClass

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ErrorClassVMDState` | const | stable keep | |
| `ErrorClassAppReference` | const | stable keep | |
| `ErrorClassDefinition` | const | stable keep | |
| `ErrorClassResource` | const | stable keep | |
| `ErrorClassService` | const | stable keep | |
| `ErrorClassServicePreempt` | const | stable keep | |
| `ErrorClassTimeResolution` | const | stable keep | |
| `ErrorClassAccess` | const | stable keep | |
| `ErrorClassInitiate` | const | stable keep | |
| `ErrorClassConclude` | const | stable keep | |
| `ErrorClassCancel` | const | stable keep | |
| `ErrorClassFile` | const | stable keep | |
| `ErrorClassOthers` | const | stable keep | |

### Enum Constants — VMDLogicalStatus

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `VMDLogicalStatusStateChangesAllowed` | const | stable keep | |
| `VMDLogicalStatusNoStateChanges` | const | stable keep | |
| `VMDLogicalStatusLimited` | const | stable keep | |

### Enum Constants — VMDPhysicalStatus

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `VMDPhysicalStatusOperational` | const | stable keep | |
| `VMDPhysicalStatusPartiallyOper` | const | keep but rename | Consider `VMDPhysicalStatusPartiallyOperational` (unabbreviated). |
| `VMDPhysicalStatusInoperable` | const | stable keep | |
| `VMDPhysicalStatusNeedsCommissioning` | const | stable keep | |

### Enum Constants — AuthMechanism

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `AuthMechanismUnknown` | const | stable keep | Zero value = unknown (deliberate). |
| `AuthMechanismNone` | const | stable keep | |
| `AuthMechanismACSEPassword` | const | stable keep | |
| `AuthMechanismTLSCertificate` | const | stable keep | |

### Object Naming Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ObjectName` | struct | stable keep | Scope + Domain + ItemID. Clear semantics. |
| `AccessSelector` | struct | stable keep | One-of: Component / Index / IndexRange. |
| `IndexRange` | struct | stable keep | Start + Count. |
| `VariableSpec` | struct | stable keep | ObjectName + optional AlternateAccess. |
| `SelectComponent(name)` | func | stable keep | Constructor helper for AccessSelector. |
| `SelectIndex(i)` | func | stable keep | Constructor helper for AccessSelector. |
| `SelectRange(low, count)` | func | stable keep | Constructor helper for AccessSelector. |

### Server Identity & Status Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ServerIdentity` | struct | stable keep | Vendor + Model + Revision. |
| `ServerStatus` | struct | stable keep | Logical + Physical status. |

### InformationReport Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `InformationReportIndication` | struct | stable keep | Client-side received report. |
| `InformationReportHandler` | func type | stable keep | Callback type for report handling. |
| `InformationReportRequest` | struct | stable keep | Server-side outgoing report. |

### Journal Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `JournalEntry` | struct | stable keep | Shared between client and server (protocol-defined shape). |
| `JournalVariable` | struct | stable keep | Tag + Value within a journal entry. |
| `JournalResult` | struct | stable keep | Entries + MoreFollows. |

### File Service Types (Server-side)

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `FileHandle` | type (any) | stable keep | Opaque handle for FileProvider. |
| `FileEntry` | struct | stable keep | Server-side file entry (Name + Size + LastModified). |
| `FileAttributes` | struct | stable keep | Open result metadata. |
| `FileListRequest` | struct | stable keep | FileSpec + ContinueAfter + MaxEntries. |
| `FileListResult` | struct | stable keep | Entries + MoreFollows. |

### Authentication Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ApplicationReference` | struct | stable keep | APTitle + AEQualifier. |
| `AuthContext` | struct | stable keep | Complete authentication context for Authenticator. |
| `AuthContext.HasTLSCertificate()` | method | stable keep | Convenience predicate. |
| `AuthContext.HasCallingApplication()` | method | stable keep | Convenience predicate. |
| `AuthResult` | struct | stable keep | Accept + Token. |

### Error Sentinels

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ErrClosed` | var (error) | stable keep | Connection closed. |
| `ErrInvokeTimeout` | var (error) | stable keep | Invoke timeout. |
| `ErrConnectionRejected` | var (error) | stable keep | Association rejected. |
| `ErrAssociationFailed` | var (error) | stable keep | Association failure. |
| `ErrNegotiationFailed` | var (error) | stable keep | Negotiation failure. |
| `ErrInvalidPDU` | var (error) | stable keep | Invalid PDU. |
| `ErrDecodeFailed` | var (error) | stable keep | Decode failure. |
| `ErrUnsupported` | var (error) | stable keep | Unsupported operation. |
| `ErrServiceRejected` | var (error) | stable keep | Service rejected (ConfirmedError). |
| `ErrDataAccess` | var (error) | stable keep | Per-variable data access error. |
| `ErrProtocol` | var (error) | stable keep | Protocol-level error. |
| `ErrServerConnClosed` | var (error) | stable keep | Server connection closed. |
| `ErrAuthenticationFailed` | var (error) | stable keep | Authentication failure. |
| `ErrFileAccessDenied` | var (error) | stable keep | File permission error for FileProvider mapping. |

### Error Types

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ServiceError` | struct | stable keep | ConfirmedErrorPDU representation. Wraps `ErrServiceRejected`. |
| `ServiceError.Error()` | method | stable keep | Implements `error`. |
| `ServiceError.Unwrap()` | method | stable keep | Implements `errors.Unwrap`. |
| `DecodeError` | struct | stable keep | Decode failure with offset/tag context. Wraps `ErrDecodeFailed`. |
| `DecodeError.Error()` | method | stable keep | Implements `error`. |
| `DecodeError.Unwrap()` | method | stable keep | Implements `errors.Unwrap`. |
| `DataAccessError` | struct | stable keep | Per-variable access error. Wraps `ErrDataAccess`. |
| `DataAccessError.Error()` | method | stable keep | Implements `error`. |
| `DataAccessError.Unwrap()` | method | stable keep | Implements `errors.Unwrap`. |
| `ProtocolError` | struct | stable keep | Layer-tagged protocol error. Wraps `ErrProtocol`. |
| `ProtocolError.Error()` | method | stable keep | Implements `error`. |
| `ProtocolError.Unwrap()` | method | stable keep | Implements `errors.Unwrap`. |
| `AuthenticationError` | struct | stable keep | Authentication failure. Wraps `ErrAuthenticationFailed`. |
| `AuthenticationError.Error()` | method | stable keep | Implements `error`. |
| `AuthenticationError.Unwrap()` | method | stable keep | Implements `errors.Unwrap`. |

### String Methods (on enum types)

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `ObjectClass.String()` | method | stable keep | Human-readable name. |
| `ValueType.String()` | method | stable keep | Human-readable name. |
| `DataAccessErrorCode.String()` | method | stable keep | Human-readable name. |
| `ErrorClass.String()` | method | stable keep | Human-readable name. |
| `VMDLogicalStatus.String()` | method | stable keep | Human-readable name. |
| `VMDPhysicalStatus.String()` | method | stable keep | Human-readable name. |
| `ObjectScope.String()` | method | stable keep | Human-readable name. |
| `AuthMechanism.String()` | method | stable keep | Human-readable name. |

---

## Transport Package (`github.com/otfabric/go-mms/transport/iso`)

| Symbol | Kind | Classification | Notes |
|--------|------|----------------|-------|
| `DialTCP(ctx, addr, opts...)` | func | stable keep | Transport-only dial. Returns `mms.Transport`. |
| `Dial(ctx, addr, opts...)` | func | stable keep | Convenience: transport + MMS client in one call. |
| `Listen(addr, opts...)` | func | stable keep | Creates a `Listener` bound to TCP address. |
| `NewListener(ln, opts...)` | func | stable keep | Wraps existing `net.Listener`. Useful for testing. |
| `Listener` | struct | stable keep | Server-side transport listener. Implements `mms.TransportListener`. |
| `Listener.Accept(ctx)` | method | stable keep | Auto-retries on handshake failures. |
| `Listener.Close()` | method | stable keep | Closes underlying TCP listener. |
| `Listener.Addr()` | method | stable keep | Returns listener address. |
| `Options` | struct | stable keep | Grouped transport configuration. Unexported fields. |
| `Option` | func type | stable keep | Functional option pattern. |
| `WithCallingTSelector(sel)` | func | stable keep | COTP calling TSAP selector. |
| `WithCalledTSelector(sel)` | func | stable keep | COTP called TSAP selector. |
| `WithClientDialOptions(opts)` | func | stable keep | Passes MMS options through Dial. |
| `WithTLSConfig(cfg)` | func | stable keep | Enables TLS transport. |
| `WithLogger(l)` | func | stable keep | Logger for handshake failures. |

---

## Special Focus Areas

### Read API Surface

| Method | Purpose | Assessment |
|--------|---------|------------|
| `Read(ctx, ReadRequest)` | Single variable by DomainID+ItemID | Clear and simple. Domain-only scope. |
| `ReadMultiple(ctx, []ObjectName)` | Multiple variables by ObjectName | Core primitive. All scopes. |
| `ReadObject(ctx, ObjectName)` | Single variable by ObjectName | Thin wrapper over ReadMultiple. Overlaps with Read but supports all scopes. |
| `ReadVariables(ctx, []VariableSpec)` | Multiple with alternate access | Full-featured. Superset of ReadMultiple. |
| `ReadComponent(ctx, name, comp)` | Structure component by name | Convenience shorthand. |
| `ReadByIndex(ctx, name, idx)` | Element by index (struct or array) | Works for both struct and array. |
| `ReadArrayElement(ctx, name, idx)` | Array element by index | **Identical wire encoding** to ReadByIndex. |
| `ReadArrayRange(ctx, name, start, count)` | Array slice | Distinct functionality. |
| `ReadNamedVariableList(ctx, name, opts...)` | All NVL members | Distinct addressing form. |

**Assessment:** The surface is clear and differentiated except for `ReadByIndex` vs `ReadArrayElement`.
These two methods have identical implementations and wire encodings. Consider:
- Deprecating `ReadArrayElement` in favor of `ReadByIndex` (which documents dual struct/array use), or
- Keeping both but adding cross-references in documentation to clarify they are aliases.

`ReadObject` vs `Read` is a deliberate scope trade-off: `Read` is the simple domain-scope fast path,
`ReadObject` is the all-scopes variant. This is a good layered API.

### Write API Surface

| Method | Purpose | Assessment |
|--------|---------|------------|
| `Write(ctx, WriteRequest)` | Single variable by DomainID+ItemID | Clear and simple. Domain-only scope. |
| `WriteObject(ctx, ObjectName, *Value)` | Single variable by ObjectName | All scopes. Parallels ReadObject. |
| `WriteVariables(ctx, []VariableSpec, []*Value)` | Multiple with alternate access | Full-featured. |
| `WriteComponent(ctx, name, comp, val)` | Structure component by name | Convenience shorthand. |
| `WriteArrayElement(ctx, name, idx, val)` | Array element by index | No `WriteByIndex` counterpart (unlike read side). |
| `WriteNamedVariableList(ctx, name, []*Value)` | All NVL members | Distinct addressing form. |

**Assessment:** The write surface is well-differentiated. No overlapping methods.
Minor inconsistency: `ReadByIndex` exists but there is no `WriteByIndex`. This is acceptable since
the read side's dual struct/array indexing is a more common use case.

### Naming Consistency

| Question | Assessment |
|----------|------------|
| Is `ObjectName` vs `VariableSpec` clear? | **Yes.** `ObjectName` is a pure name (scope + domain + item). `VariableSpec` adds alternate access selectors. The naming mirrors MMS ASN.1 terminology. |
| Is `ReadByIndex` vs `ReadArrayElement` clear? | **No.** These are functionally identical. The distinction adds API surface without adding capability. `ReadByIndex` is the better name because it works for both structs and arrays. |
| Is `ShallowCompatible` the right name? | **Acceptable but non-obvious.** The name correctly suggests a shallow (non-recursive) check, but "compatible" is vague. Consider renaming to `MatchesType` or documenting more prominently what "shallow" means (checks top-level type and element count only). |
| `ClientStatusRequest` vs other `*Options` types? | **Minor inconsistency.** Other optional-parameter types use `*Options` suffix (`FileOpenOptions`, `ReadNamedVariableListOptions`). This one uses `*Request` even though it configures optional behavior. Consider renaming to `StatusOptions`. |
| `DataAccessErrorTemporarilyUnavail` | **Abbreviated.** Should be `DataAccessErrorTemporarilyUnavailable` for consistency. |
| `DataAccessErrorObjectAccessUnsup` | **Abbreviated.** Should be `DataAccessErrorObjectAccessUnsupported` for consistency. |
| `VMDPhysicalStatusPartiallyOper` | **Abbreviated.** Should be `VMDPhysicalStatusPartiallyOperational` for consistency. |

### Zero-value Safety

| Type | Zero-value Safe? | Notes |
|------|-----------------|-------|
| `DialOptions` | **Yes** | All sub-structs use zero → library defaults. |
| `MMSOptions` | **Yes** | Zero → 65000 PDU, 5 outstanding, 10 nesting. |
| `ISOOptions` | **Yes** | Zeros mean omit optional selectors. |
| `TransportOptions` | **Yes** | Nil selectors mean omit. |
| `ServerOptions` | **Yes** | nil Logger/Authenticator/FileProvider handled. |
| `ServerMMSOptions` | **Yes** | Same defaults as client side. |
| `FileOpenOptions` | **Yes** | Zero → position 0 (start of file). |
| `ReadNamedVariableListOptions` | **Yes** | false → no spec-with-result. |
| `ClientStatusRequest` | **Yes** | false → non-extended derivation. |
| `FileDirectoryRequest` | **Yes** | Empty strings → list all, first page. |
| `NameListRequest` | **Yes** | Zero ObjectClass → NamedVariable, zero Scope → VMD. |
| `ObjectName` | **Partial** | Zero Scope → VMD (correct). But zero ItemID fails validation. |
| `ReadRequest` | **No** | Requires DomainID and ItemID; validated at call site. |
| `WriteRequest` | **No** | Requires DomainID, ItemID, and Value; validated at call site. |

All option/configuration structs are zero-value safe. Request structs that require
field values are validated early with clear error messages.

---

## Recommendations Summary

### Before v0.1.0

1. **Deprecate `ReadArrayElement`** — it is byte-identical to `ReadByIndex`. Keep `ReadByIndex`
   and document it works for both arrays and structures. If both are kept, add explicit
   cross-references in godoc.

2. **Unabbreviate constants:**
   - `DataAccessErrorTemporarilyUnavail` → `DataAccessErrorTemporarilyUnavailable`
   - `DataAccessErrorObjectAccessUnsup` → `DataAccessErrorObjectAccessUnsupported`
   - `VMDPhysicalStatusPartiallyOper` → `VMDPhysicalStatusPartiallyOperational`

3. **Clarify `NVLAccessResult`** — this type is exported but not returned by any public method.
   Either wire it into `ReadNamedVariableList` when `SpecificationWithResult` is true, or
   remove it.

4. **Document `Value.Get` limitation** — component-by-name access always returns an error
   saying TypeSpec context is needed. Document this prominently, or remove component
   support from `Get` and keep it index-only.

### Before v1.0

5. **Consider renaming `ClientStatusRequest`** → `StatusOptions` for consistency with
   `FileOpenOptions` and `ReadNamedVariableListOptions`.

6. **Document `ShallowCompatible` semantics** — add a one-liner explaining it checks
   type match + element count but does not recurse. Consider renaming to `MatchesTopLevel`.

7. **Document `ReadObject` vs `Read` trade-off** — add cross-references explaining that
   `Read` is the domain-scope fast path and `ReadObject` supports all scopes.

8. **Audit `doc.go` "out of scope" section** — it still lists file and journal services
   as out of scope, but they are now implemented. Update the doc.

### API Qualities (no action needed)

- **Error hierarchy** is excellent: sentinel errors for `errors.Is`, typed errors for
  `errors.As`, proper `Unwrap` chains.
- **Concurrency model** is clean: documented mutex strategy, idempotent Close/Abort.
- **Transport abstraction** is well-designed: interface + optional interfaces via type assertion.
- **Functional options** in `transport/iso` follow Go best practices.
- **Value type** is properly opaque with defensive copying on all accessors and constructors.
- **Server model** is well-structured: register-then-serve, handler callbacks, connection snapshots.
- **Authentication system** is complete and extensible: mechanism enum, typed context,
  opaque token pass-through.
