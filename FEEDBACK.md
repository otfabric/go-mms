
Abort PDU (server→client) — The server detects client disconnects and handles ISO Release, but does not proactively send an MMS Abort PDU to the client. Client-initiated Abort is fully implemented.

Association-scope object listing — Association-scoped variables and NVLs can be stored and accessed, but GetNameList for association scope returns service-not-supported. Listing and lifecycle management for association-scope objects is deferred.

DeleteNamedVariableList scope — Only scopeOfDelete=0 (specific list names) is supported. Scope-based bulk deletion (e.g., delete all domain NVLs) is not implemented.

No live-wire interop testing — The interop tests validate encoding compatibility via inline byte patterns. There is no automated test harness that connects to a real MMS server or C reference implementation.

Semaphore, Event, Program Invocation services — These MMS services (ISO 9506 Part 2) are not implemented. The ObjectClass enum includes them for GetNameList classification, but no client methods or server handlers exist.

Unsolicited Status — The MMS UnsolicitedStatus service is not implemented. Only request/response Status is supported.

Cancel service — The MMS Cancel service (to cancel an in-flight confirmed request) is not implemented.

Segmented file transfer — ObtainFile delegates to the FileProvider as a single synchronous operation. The MMS-specified segmented transfer protocol (where the server calls back to the client's file services) is not implemented.

----


Now I have a thorough picture. Here is my analysis:

---

## Gap-by-Gap Assessment Against C Reference (libIEC61850)

### 1. Abort PDU (server→client)

**C status: Not implemented either.** The C server's `MmsServer_abortConnection` (line 931 of `mms_server.c`) simply calls `IsoServer_closeConnection` — it tears down the TCP/ISO connection without sending an MMS Abort PDU. The PDU dispatcher in `mms_server_connection.c` has no code to *send* an Abort PDU, only to silently ignore a received Reject PDU (tag `0xa4`). The C server does handle Conclude (request/response) but not Abort.

**Effort to implement in Go:** Low-medium. It's a simple unconfirmed PDU (tag `0xa0`, just an optional reason integer). The Go code already has the Abort encoder on the client side — the server just needs to send it before closing the transport. Not blocked by missing C reference code; can be implemented directly from the standard. ~50-100 lines.

---

### 2. Association-scope object listing (GetNameList)

**C status: Implemented.** The C server fully supports association-scoped NVL listing (`OBJECT_SCOPE_ASSOCIATION = 2`) in `mms_get_namelist_service.c` lines 810-828 — it calls `getNamedVariableListAssociationSpecific(connection)` and returns the name list. This is gated behind compile-time flags `MMS_DATA_SET_SERVICE` and `MMS_DYNAMIC_DATA_SETS`.

**Effort to implement in Go:** Medium. The C code is relatively straightforward — ~20 lines for the name list case. The main work is on the Go server model side: the server already stores association-scoped NVLs per connection, so you just need to wire up the listing through `GetNameList`. Roughly ~100-150 lines of Go, mostly following existing patterns for VMD/domain scope.

---

### 3. DeleteNamedVariableList scope

**C status: Only `scopeOfDelete=specific` is implemented.** In `mms_named_variable_list_service.c` lines 153-267, the C code parses `scopeOfDelete`, but the handler at line 264 shows:

```c
else {
    mmsMsg_createServiceErrorPdu(invokeId, response, MMS_ERROR_ACCESS_OBJECT_ACCESS_UNSUPPORTED);
}
```

So scopes `aaspecific` (1), `domain` (2), and `vmd` (3) all return "unsupported" in the C code too. Only `specific` (0) is implemented, exactly like the Go version.

**Effort to implement in Go:** Low, but arguably not worth it — the C reference doesn't implement it either, and it's rarely used in practice. If needed, ~100-200 lines to iterate and delete all NVLs matching the given scope.

---

### 4. No live-wire interop testing

**C status: N/A — this is an infrastructure/test gap**, not a protocol feature. The C reference is itself one of the implementations you'd test *against*, not something that provides a test harness.

**Effort:** This requires setting up a test environment (Docker container with the C server, automated Go test client), not porting C code. Medium-high effort for the infrastructure, but no code to port.

---

### 5. Semaphore, Event, Program Invocation services

**C status: Not implemented.** The C code defines the `ObjectClass` enum values for these (e.g., `eventCondition = 5`, `programInvocation = 10`) and the `ServiceSupportOptions` bit positions, but there are **zero service handlers, zero client methods, and zero server logic** for Semaphore, Event Condition, Event Action, Event Enrollment, or Program Invocation services. The "Semaphore" references in the C source are OS-level mutexes (POSIX/threading primitives), not MMS Semaphore services.

**Effort to implement in Go:** Very high, and there's **no C code to port**. These are full MMS service groups with complex state machines (event conditions have trigger monitoring, enrollments, notifications). Implementing them from the ISO 9506-2 standard alone would be a major undertaking (thousands of lines). In practice, most IEC 61850 deployments don't use these services — they're legacy MMS features.

---

### 6. Unsolicited Status

**C status: Not implemented.** The constant `MMS_SERVICE_UNSOLICITED_STATUS` is defined (0x02) in `mms_association_service.c` but **never used** — it's not OR'd into the `servicesSupported` bitstring during initiation, and there's no handler or sender for it. The C server only implements request/response Status (via `mmsServer_handleStatusRequest`).

**Effort to implement in Go:** Low. It's an unconfirmed service (similar to InformationReport) — the server sends an unsolicited `UnconfirmedPDU` containing VMD status. ~50-80 lines, following the existing InformationReport pattern. No C code to port, but straightforward from the standard.

---

### 7. Cancel service

**C status: Not implemented.** The C code declares `MMS_SERVICE_CANCEL = 0x08` and even includes it in the `servicesSupported` bitstring during initiation (line 126: `| MMS_SERVICE_CANCEL`), but there is **no handler** for Cancel PDUs in the server dispatcher (`mms_server_connection.c`). The ASN.1 generated code (`RejectPDU.c`) has the `cancelRequestPDU` reject reason, but that's just ASN.1 struct definitions, not actual service logic.

**Effort to implement in Go:** Medium. Cancel requires tracking in-flight invoke IDs and being able to abort the processing of a confirmed request. The PDU encoding is simple (just the invoke ID to cancel), but the concurrency/lifecycle coordination in the Go server to actually interrupt an in-progress handler makes this non-trivial. No C code to port. ~200-400 lines, depending on how thoroughly you support interruption.

---

### 8. Segmented file transfer (ObtainFile)

**C status: Fully implemented.** This is the one big gap where the C code has a **complete implementation** that could serve as a porting reference. The C server implements ObtainFile as a multi-step state machine in `mms_file_service.c` (lines 430-820):

1. Receives `ObtainFileRequest` from client
2. Sends `FileOpenRequest` back to the client (role reversal — server calls client's file services)
3. Iterates with `FileReadRequest` to download chunks
4. Sends `FileCloseRequest` when done
5. Sends `ObtainFileResponse` or `ObtainFileError`

The state machine has ~8 states (`FILE_OPEN_SENT`, `SEND_FILE_READ`, `FILE_READ_SENT`, `SEND_FILE_CLOSE`, `FILE_CLOSE_SENT`, etc.) with timeouts. The client side (`mms_client_files.c`, `mms_client_connection.c`) handles receiving the server's file requests during an active ObtainFile operation.

**Effort to implement in Go:** High. This is ~400 lines of C state machine logic that would translate to ~300-500 lines of Go. The key complexity is the role reversal: during an ObtainFile, the server sends confirmed requests *to the client*, and the client must handle them. The Go architecture currently doesn't support the server sending confirmed requests or the client handling incoming requests — this requires new connection-level plumbing. Estimated ~500-800 lines total including the infrastructure changes.

---

## Summary Table

| # | Gap | In C? | C Code to Port? | Go Effort | Priority |
|---|-----|:-----:|:---------------:|:---------:|:--------:|
| 1 | Abort PDU (server→client) | No | None | Low | Nice-to-have |
| 2 | Association-scope GetNameList | **Yes** | ~20 lines | Medium | Useful |
| 3 | DeleteNVL bulk scope | No | None | Low | Not needed |
| 4 | Live-wire interop testing | N/A | N/A | Medium-high | Infrastructure |
| 5 | Semaphore/Event/ProgramInvoc | No | None | Very high | Not needed |
| 6 | Unsolicited Status | No | None | Low | Nice-to-have |
| 7 | Cancel service | No | None | Medium | Rare in practice |
| 8 | Segmented ObtainFile | **Yes** | ~400 lines | High | Valuable |

Key takeaways:

- **Gaps 3, 5, 6, 7** are also missing from the C reference — these are not regressions vs. the C implementation.
- **Gap 2** (association-scope listing) is the easiest win with existing C code to reference.
- **Gap 8** (segmented ObtainFile) is the most substantial feature gap — the C code has a full implementation, but porting it requires architectural changes to Go's server/client connection model to support role-reversal (server sending confirmed requests to clients).
- **Gap 1** (server Abort) is trivial but the C code doesn't do it either, so there's no interop expectation.