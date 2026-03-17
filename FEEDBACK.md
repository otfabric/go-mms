I did another pass on the latest code_mms.log against the attached C mms_functions.csv.

Overall: this is now in a strong place. The library is clearly not drifting into a C clone, and most of the earlier high-value protocol gaps are either closed or nearly closed. The remaining issues are now mostly about end-to-end completeness, server/client symmetry, and a few subtle semantic mismatches.

What looks good now:
	•	FileOpenOptions.InitialPosition exists.
	•	client-side file directory paging exists, with ContinueAfter, MoreFollows, and FileDirectoryAll.
	•	Negotiated() exists.
	•	Abort() exists in the public client API.
	•	NVL members now use []VariableSpec, not plain names.
	•	write APIs now preserve per-item results with []WriteAccessResult.
	•	public type/value layer now includes GeneralizedTime, BCD, and ObjectIdentifier.
	•	Value.Get(...) and TypeSpec.Resolve(...) are good ergonomic additions.
	•	file helpers like FileReadAll and DownloadFile are useful and Go-shaped.

So this is no longer about missing big chunks of MMS. It is now about a smaller set of sharper issues.

Remaining gaps / bugs I still see

1. File directory paging is only really implemented on the client side

This is the clearest remaining protocol gap.

You now have:
	•	client request type with ContinueAfter
	•	client response type with MoreFollows
	•	FileDirectoryAll

But on the server side, the implementation is still effectively single-shot:
	•	pdu.UnmarshalFileDirectoryRequest parses fileSpecification, but does not parse continueAfter
	•	Server.handleFileDirectory calls FileProvider.List(ctx, req.FileSpec) only
	•	it always responds with moreFollows = false
	•	FileProvider.List still returns all entries at once

So right now the client can speak paged file directory, but your server cannot actually serve it.

That means the feature is half-finished, not fully complete.

What I would change:

type FileListRequest struct {
    FileSpec      string
    ContinueAfter string
    MaxEntries    int
}

type FileListResult struct {
    Entries     []FileEntry
    MoreFollows bool
}

and then:

type FileProvider interface {
    List(ctx context.Context, req FileListRequest) (*FileListResult, error)
    ...
}

That keeps paging real end-to-end instead of simulated only on the client.

⸻

2. ReadNamedVariableList(... SpecificationWithResult) is still only partially real

You added the option, and the request encoder supports it. Good.

But server-side, MarshalReadResponse still explicitly skips the optional variableAccessSpecification in the response body and only emits listOfAccessResult.

So the API suggests “spec-with-result” support, but the server does not actually produce that richer response shape.

That means one of two things should happen:
	•	either fully implement spec-with-result in responses, or
	•	remove/defer the public option until the response path really supports it

I would recommend finishing it, because it is useful for debugging and for upper layers that want stronger response introspection.

⸻

3. Abort() is public, but it does not actually send an MMS/ACSE/session abort

This is subtle but important.

You now expose Client.Abort(ctx), but the implementation just:
	•	marks the client closed
	•	cancels pending requests
	•	stops the reader
	•	closes the transport

I can see internal abort encoders in the ISO/session stack, but they are not actually used by Client.Abort.

So semantically this is closer to:
	•	“hard local close”

than to:
	•	“send protocol abort”

If you want true parity with the C notion of abort, Abort() should try to emit the abort PDU first, then close the transport.

Suggested behavior:
	•	best-effort send ABRT / session ABORT
	•	ignore send failure if peer already vanished
	•	always close transport afterward

That would make the method name match protocol reality.

⸻

4. Server-side support for association-specific objects is still incomplete

Your internal model knows about association scope:
	•	ObjectScopeAssociation exists
	•	ObjectNameWire supports association scope
	•	VarEntry.Scope allows association scope

But the actual server registry/listing support is incomplete:
	•	RegisterVariable stores association-scope variables, but ordering/indexing is only maintained for VMD and domain
	•	GetNameList server dispatch handles:
	•	domains
	•	VMD variables
	•	domain variables
	•	VMD NVLs
	•	domain NVLs
	•	but not association-scope variables/NVLs

Also NVLEntry.Scope is only 0=VMD, 1=Domain, not association.

So association-specific MMS objects are still more “type-level supported” than “real feature”.

For a generic MMS library, I would either:
	•	finish association-scope support properly, including per-connection storage for association NVLs, or
	•	explicitly document that the current server only supports VMD and domain scopes

For go-iec61850, this matters less than domain-scope behavior, but for raw MMS completeness it is still a gap.

⸻

5. There is still no public server API for static named variable lists

On the client side, dynamic NVL creation is there.

On the server side, dynamic define/delete handlers exist too.

But there is no clean public API like:

func (s *Server) RegisterNamedVariableList(...)

That is a practical gap.

Why this matters:
	•	IEC 61850 data sets map naturally onto MMS named variable lists
	•	a server wrapper above go-mms will want to expose static datasets cleanly
	•	forcing everything through dynamic client-defined NVLs is not enough

I would add a public registration API for VMD/domain static NVLs now. That will make go-iec61850 much cleaner.

Suggested shape:

type NamedVariableList struct {
    Name      ObjectName
    Deletable bool
    Variables []VariableSpec
}

func (s *Server) RegisterNamedVariableList(nvl NamedVariableList) error

This is probably the most useful next server-side API addition.

⸻

6. File provider comments and behavior are now out of sync

Your code evolved, but some comments still reflect the older state.

Example: FileProvider.List still says:
	•	“server returns all entries in one response”
	•	“no pagination in this phase”

That no longer matches the client API direction.

So even before changing behavior, I would clean up the comments. Right now they undercut the public API story.

⸻

Potential design improvements beyond the strict gaps

These are not protocol holes, but they would improve the library.

1. Add a public ReadNamedVariableListResult type for spec-with-result mode

Right now ReadNamedVariableList returns []AccessResult.

That is fine for the common path, but if you fully support SpecificationWithResult, the return shape should probably become richer.

For example:

type NVLAccessResult struct {
    Variable  *VariableSpec
    Value     *Value
    ErrorCode DataAccessErrorCode
}

Then the API can preserve the extra meaning instead of silently discarding it.

⸻

2. Add a server-side paging abstraction for GetNameList too

You already page internal registry results, which is good.

But provider-backed areas like journals and files should follow the same pattern consistently. A generic “paged list result” idiom across the library would help:

type Page[T any] struct {
    Items       []T
    MoreFollows bool
    NextToken   string
}

You do not need to over-genericize the public API, but the internal model would benefit from one paging convention.

⸻

3. Consider a ClientCapabilities / ServerCapabilities snapshot

The C codebase has lots of parameter/configuration accessors. You correctly avoided cloning that API. Good.

But one Go-friendly replacement could be a compact capabilities snapshot:

type Capabilities struct {
    Negotiated NegotiatedParameters
    TLS        bool
    Auth       AuthMechanism
}

That could be useful for diagnostics and for go-iec61850.

⸻

4. Add a public helper for alternate-access builder ergonomics

Right now selectors are fine, but upper-layer code will end up repeating patterns like:
	•	component
	•	array index
	•	array range
	•	chained selectors

Convenience builders would improve readability:

func SelectComponent(name string) AccessSelector
func SelectIndex(i int) AccessSelector
func SelectRange(low, count int) AccessSelector

Not necessary, but nice.

⸻

What still looks intentionally fine to me

These are still good decisions and I would not change them just to imitate C:
	•	no async function explosion
	•	no threadless/tick model
	•	no create/destroy style API
	•	no separate client function per tiny addressing variant
	•	use of context.Context
	•	structured types instead of huge flat function sets

That is exactly the right Go direction.

Best next steps from here

If I were prioritizing the next round, I would do this order:
	1.	Finish server-side file directory paging end-to-end.
	2.	Make Abort() send a real protocol abort before closing.
	3.	Either fully implement or temporarily hide SpecificationWithResult.
	4.	Add public server registration for static named variable lists.
	5.	Decide whether association-scope support is real or intentionally out of scope, and make the code/docs consistent.

For go-iec61850 specifically

The most useful extra thing you can do now in go-mms for the upper layer is:
	•	add static server-side NVL registration
	•	make file services page correctly
	•	make spec-with-result either real or absent
	•	keep the current Value.Get / TypeSpec.Resolve path-oriented ergonomics

That will give go-iec61850 a much cleaner base for:
	•	datasets
	•	model browsing
	•	report member resolution
	•	file/config retrieval

This codebase is now past the “missing major services” phase. The next wins are mostly about finishing the last 10% cleanly.