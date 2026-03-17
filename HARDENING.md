Below is a concrete execution plan you can hand to AI coding agents. It is optimized for your current state: first implementation done, solid coverage already in place, and go-mms intended to become the base layer for go-iec61850. The plan keeps MMS generic, prioritizes hardening before breadth, and treats interop/debuggability as first-class goals, matching your original requirements.  ￼  ￼

go-mms Next-Step Execution Plan

Goal of this plan

Turn the current go-mms implementation into a trustworthy, stable, well-proven base library before building go-iec61850 on top of it.

This means the next work should prioritize:
	1.	hardening
	2.	protocol compliance proof
	3.	API stabilization
	4.	docs and usage guidance
	5.	only then selective expansion

Do not use the next cycle for broad new feature work unless it directly serves parity, correctness, or go-iec61850 bootstrap.

⸻

Success criteria for this plan

At the end of this plan, you should have:
	•	a parser and transport core that is hard to break
	•	explicit proof of behavior against the C reference
	•	a stable public API you are comfortable depending on from go-iec61850
	•	a small set of excellent examples and docs
	•	a short list of known limitations instead of hidden uncertainty

⸻

Working rules for AI coding agents

Every agent task should follow these rules:
	•	keep go-mms MMS-only
	•	do not add IEC 61850 concepts
	•	do not expose internal wire types publicly
	•	do not widen the public API unless the task explicitly calls for it
	•	prefer strictness over leniency unless interop requires tolerance
	•	whenever behavior is ambiguous, add a test first
	•	whenever changing decode behavior, add negative tests
	•	whenever changing ownership/copy semantics, document it
	•	whenever changing public API, update examples and docs in the same change
	•	prefer small PR-sized batches over giant refactors

⸻

Execution order

Execute in this order:
	1.	Phase A — hardening and invariants
	2.	Phase B — compliance and interop proof
	3.	Phase C — API stabilization
	4.	Phase D — docs and examples
	5.	Phase E — release preparation
	6.	Phase F — only then bootstrap go-iec61850

⸻

Phase A — Hardening and invariants

Objective

Make the current implementation robust under malformed input, cancellation, concurrency, and ownership edge cases.

A1. Decoder strictness audit

Agent task: audit every unmarshal/decode path and make strictness explicit and consistent.

Target packages:
	•	internal/berutil
	•	internal/codec
	•	internal/pdu
	•	internal/acse
	•	internal/session
	•	internal/presentation
	•	internal/isostack

Checklist per decoder:
	•	reject empty required payloads
	•	reject unexpected tags
	•	reject duplicate singular fields
	•	reject missing required fields
	•	reject trailing bytes unless explicitly allowed
	•	reject malformed extended tags
	•	reject malformed length encodings
	•	reject invalid nesting / truncated TLVs

Deliverables:
	•	HARDENING_DECODER_CHECKLIST.md
	•	test additions for every bug found
	•	comments in code where lenient interop behavior is intentionally allowed

Done when:
	•	every decode function is either strict by default or explicitly documented as lenient for interop
	•	no “silent skip unknown field” behavior remains unless justified

A2. Ownership and copy-semantics audit

Agent task: make memory ownership behavior explicit and safe.

Target files:
	•	value.go
	•	mms.go
	•	client_file.go
	•	internal/pdu/data.go
	•	internal/pdu/file.go
	•	any path converting Value ↔ DataValue

Checklist:
	•	ensure byte slices are copied on ingress/egress where needed
	•	ensure file read payload ownership is clear
	•	ensure arrays/structures do not accidentally alias hidden mutable state
	•	ensure alternate-access patching cannot mutate caller-owned data unexpectedly
	•	ensure logging/raw hooks do not expose mutable shared buffers unsafely

Deliverables:
	•	OWNERSHIP.md
	•	added tests for aliasing/copy behavior
	•	doc comments on NewArray, NewStructure, accessors, conversion helpers

Done when:
	•	a user can reason about whether a returned value is owned, shared, or copied
	•	no hidden aliasing remains in protocol conversion paths

A3. Context, timeout, and shutdown semantics audit

Agent task: prove client and server behavior under cancellation and close races.

Scenarios to test:
	•	context timeout during association
	•	context timeout during confirmed request send
	•	context timeout while waiting for response
	•	Close while request is in flight
	•	late response after cancel
	•	remote disconnect during request
	•	conclude timeout
	•	close called twice concurrently
	•	server disconnect with open FRSM/file handles
	•	information report arriving during shutdown

Deliverables:
	•	TIMEOUT_AND_CLOSE.md
	•	new tests in:
	•	mms_test.go
	•	concurrency_test.go
	•	errors_test.go
	•	transport/integration tests where relevant

Done when:
	•	shutdown semantics are deterministic and documented
	•	all major lifecycle errors are stable and test-covered

A4. Concurrency/race hardening

Agent task: aggressively exercise client/server concurrency.

Focus areas:
	•	invoke tracker lifecycle
	•	sendMu and request serialization
	•	response dispatch under late/duplicate frames
	•	report handler registration vs invocation
	•	server connection registry and broadcast paths
	•	FRSM table access
	•	alternate-access read-modify-write paths

Required commands:
	•	go test -race ./...
	•	repeated stress loops for selected packages
	•	repeated package runs for flaky detection

Deliverables:
	•	more race-focused tests
	•	elimination of any data race warnings
	•	short RACE_NOTES.md

Done when:
	•	race detector is clean
	•	there are no known flaky tests

A5. Defensive bounds and resource controls

Agent task: add explicit bounds and sanity checks.

Audit:
	•	negotiated PDU size enforcement
	•	maximum decoded lengths
	•	nesting depth protection
	•	file chunk sizing policy
	•	maximum list/result allocations from peer input
	•	invoke ID reuse assumptions
	•	protection against oversized file metadata values

Deliverables:
	•	code guards
	•	boundary tests
	•	LIMITS.md

Done when:
	•	the library does not trust peer-provided sizes blindly
	•	all important limits are documented

⸻

Phase B — Compliance and interop proof

Objective

Prove the implementation behaves correctly relative to the C reference and real wire expectations.

B1. Service parity matrix

Agent task: create a machine-readable and human-readable support matrix.

Rows should include at least:
	•	Initiate
	•	Conclude
	•	Identify
	•	Status
	•	GetNameList
	•	GetVariableAccessAttributes
	•	Read
	•	Write
	•	Define/Get/Delete NVL
	•	InformationReport
	•	File services
	•	Journal services
	•	Reject / ConfirmedError handling

Columns:
	•	client supported
	•	server supported
	•	public API present
	•	unit encode tested
	•	unit decode tested
	•	negative tests present
	•	fuzzed
	•	interop tested vs C
	•	notes / known gaps

Deliverables:
	•	COMPLIANCE.md

Done when:
	•	you can answer “what is implemented and how well is it proven?” in one file

B2. Golden PDU/frame fixtures

Agent task: generate and lock golden fixtures for core services.

Priority services:
	•	initiate
	•	identify
	•	status
	•	getnamelist
	•	getvaraccess
	•	read
	•	write
	•	named variable list services
	•	file open/read/close/directory
	•	journal read where available
	•	confirmed error / reject

Sources:
	•	current Go outputs
	•	C reference outputs where obtainable

Deliverables:
	•	testdata/golden/...
	•	fixture-driven tests in internal/pdu, internal/codec, possibly transport-level tests

Done when:
	•	changes to wire encoding become deliberate and diffable

B3. Go ↔ C interop harness

Agent task: create a repeatable interop harness against the source C implementation under sources/.

Minimum goals:
	•	Go client against C server for core confirmed services
	•	optionally Go server against C client for selected paths
	•	automated or scriptable local run instructions

Deliverables:
	•	interop/ harness or scripts
	•	INTEROP.md
	•	first green interop scenarios

Done when:
	•	you have at least a thin automated proof that the Go stack talks to the C stack correctly

B4. BER/PDU fuzzing expansion

Agent task: expand fuzz coverage to high-risk parse paths.

Priority fuzz targets:
	•	BER TLV decoding
	•	confirmed request parsing
	•	confirmed response parsing
	•	file service decoding
	•	alternate access decoding
	•	initiate/association payload decoding
	•	reject / confirmed error decoding

Deliverables:
	•	more fuzz targets
	•	seed corpus in testdata/fuzz/...
	•	crash triage notes if anything fails

Done when:
	•	high-risk parsing layers are continuously fuzzable

Your original requirements explicitly called out negative tests, fuzzing, boundary tests, race tests, and interop as mandatory, especially for protocol parsing and association handling.  ￼

⸻

Phase C — API stabilization

Objective

Make the public surface intentionally stable before go-iec61850 depends on it.

C1. Public API inventory

Agent task: inventory every exported symbol and classify it:
	•	stable keep
	•	keep but rename
	•	keep but document sharper
	•	move internal
	•	deprecate before first tagged release

Deliverables:
	•	API_REVIEW.md

Special focus:
	•	Read, ReadMultiple, ReadVariables, ReadObject
	•	Write, WriteVariables, WriteObject, WriteNamedVariableList
	•	Value
	•	TypeSpec
	•	ObjectName
	•	VariableSpec
	•	error types
	•	options/config types
	•	transport abstractions

C2. Naming and surface cleanup

Agent task: remove or smooth awkward naming before versioning.

Questions to answer:
	•	is ReadByIndex clearly different enough from ReadArrayElement?
	•	is ShallowCompatible the right final name?
	•	do “object”, “variable”, and “named variable list” names feel consistent?
	•	are file and journal APIs consistent with the rest of the library?
	•	are option structs zero-value safe where reasonable?

Deliverables:
	•	final naming decisions
	•	minimal cleanup PRs
	•	matching doc/example updates

C3. Error taxonomy freeze

Agent task: define the stable error model.

Need clear distinction between:
	•	invalid local usage
	•	transport failure
	•	association rejection
	•	negotiation/protocol failure
	•	remote confirmed service error
	•	per-variable data access error
	•	unsupported feature
	•	timeout/cancellation
	•	closed connection

Deliverables:
	•	ERRORS.md
	•	examples using errors.Is / errors.As
	•	tests that lock expected behavior

Your original requirements were explicit that the API should be strongly typed, avoid leaking wire artifacts, and maintain clear error boundaries.  ￼

C4. Observability polish

Agent task: standardize logging and trace behavior.

Make sure you support documented levels such as:
	•	disabled
	•	lifecycle
	•	protocol summary
	•	raw/wire trace

Checklist:
	•	invoke IDs in relevant logs
	•	association and negotiation traces
	•	optional frame/PDU hooks
	•	no noisy logging by default
	•	redaction awareness for auth-sensitive data

Deliverables:
	•	OBSERVABILITY.md
	•	example logger wiring
	•	tests where practical

This directly matches the observability expectations in the requirements.  ￼

⸻

Phase D — Docs and examples

Objective

Make the library easy to adopt without reading the full source tree.

D1. README rewrite

Agent task: rewrite the README as a serious product-facing entry point.

Must include:
	•	what go-mms is
	•	what it supports now
	•	what is intentionally out of scope
	•	relationship to go-tpkt, go-cotp, future go-iec61850
	•	client/server maturity
	•	safety note about MMS genericity vs IEC 61850 layering
	•	quick start snippets

D2. Package docs pass

Agent task: improve doc.go and key exported symbol docs.

Priority docs:
	•	root package
	•	Transport and listener abstractions
	•	Value
	•	TypeSpec
	•	ObjectName
	•	file services
	•	journal services
	•	server usage notes
	•	concurrency and ownership notes

D3. Examples upgrade

Agent task: turn _examples into canonical reference examples.

Add or polish examples for:
	•	basic client connect / identify / status
	•	read/write variable
	•	get name list with pagination
	•	alternate access
	•	named variable list usage
	•	information report handling
	•	file open/read/close
	•	server basic usage

D4. Known limitations and support matrix

Agent task: publish explicit current boundaries.

Deliverables:
	•	KNOWN_LIMITATIONS.md
	•	SUPPORTED_SERVICES.md or folded into COMPLIANCE.md

⸻

Phase E — Release preparation

Objective

Prepare the first real tagged release of go-mms.

E1. Release checklist

Agent task: create and execute release checklist.

Include:
	•	all tests green
	•	race detector green
	•	fuzz targets stable
	•	docs updated
	•	examples build/run
	•	interop baseline documented
	•	public API reviewed
	•	known limitations documented

Deliverables:
	•	RELEASE_CHECKLIST.md

E2. Versioning decision

Agent task: decide whether to tag as:
	•	pre-1.0 experimental but serious
	•	or 1.0 if you believe public API is stable enough

My bias: use a conservative first public stability posture unless interop and API polish are both strong.

⸻

Phase F — Bootstrap go-iec61850

Objective

Use go-mms in one narrow vertical slice before expanding.

Recommended first slice:
	•	association
	•	identify/status
	•	get name list
	•	basic read/write
	•	typed value conversion needed by IEC 61850 wrapper

Do not start go-iec61850 by demanding every remaining MMS corner case.

⸻

Suggested issue / task breakdown for AI agents

Use these as separate agent-driven work batches.

Batch 1

“Run strictness audit on internal/pdu and internal/codec, add negative tests for trailing bytes, duplicate fields, missing required fields, malformed extended tags, and document any intentional lenient interop paths.”

Batch 2

“Audit value/data ownership and copying across value.go, mms.go, internal/pdu/data.go, internal/pdu/file.go, and file APIs. Fix unsafe aliasing, add tests, and write OWNERSHIP.md.”

Batch 3

“Expand lifecycle tests for timeout, cancellation, and close races across client/server/request dispatch. Produce TIMEOUT_AND_CLOSE.md.”

Batch 4

“Add race/stress tests for invoke tracking, response dispatch, report handlers, server connection registry, FRSM handling, and alternate-access write patching.”

Batch 5

“Create COMPLIANCE.md service matrix covering client/server support, tests, fuzzing, negative cases, and interop status.”

Batch 6

“Build golden PDU fixtures for initiate, identify, status, getnamelist, getvaraccess, read, write, NVL, file services, and errors. Use testdata/golden and fixture-driven tests.”

Batch 7

“Create a minimal Go↔C interop harness using the sources/ tree and document it in INTEROP.md.”

Batch 8

“Perform exported API inventory and create API_REVIEW.md with keep/rename/internal/deprecate recommendations.”

Batch 9

“Standardize logging and tracing behavior, add documentation for observability levels, invoke ID tracing, raw hooks, and redaction guidance.”

Batch 10

“Rewrite README, improve package docs, and upgrade examples into canonical reference examples.”

⸻

Concrete repo deliverables to create next

Create these files in roughly this order:
	1.	HARDENING_DECODER_CHECKLIST.md
	2.	OWNERSHIP.md
	3.	TIMEOUT_AND_CLOSE.md
	4.	LIMITS.md
	5.	RACE_NOTES.md
	6.	COMPLIANCE.md
	7.	INTEROP.md
	8.	API_REVIEW.md
	9.	ERRORS.md
	10.	OBSERVABILITY.md
	11.	KNOWN_LIMITATIONS.md
	12.	RELEASE_CHECKLIST.md

⸻

Timeboxing advice

Use this rough split:
	•	40% hardening
	•	25% compliance/interop
	•	15% API review
	•	15% docs/examples
	•	5% release prep

That is the right balance for a base library.

⸻

What not to do next

Do not spend the next cycle on:
	•	large new MMS feature expansion
	•	IEC 61850 abstractions
	•	broad server feature ambitions beyond proving current behavior
	•	premature optimization without profiling
	•	cosmetic refactors with no correctness payoff

Your requirements explicitly warned against mixing IEC 61850 in, mirroring C too closely, exposing ASN.1 artifacts, and growing broad APIs before core behavior is solid.  ￼

Recommended immediate first three actions

Start with these, in order:
	1.	strictness audit in internal/pdu and internal/codec
	2.	lifecycle/race hardening around request dispatch and close behavior
	3.	COMPLIANCE.md plus golden fixture baseline

That sequence will give you the fastest increase in confidence before go-iec61850 starts depending on this layer.

If you want, I can turn this into a ready-to-commit NEXT_STEPS.md with issue-sized checklists.