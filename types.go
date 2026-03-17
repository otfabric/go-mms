package mms

import (
	"context"
	"crypto/tls"
	"encoding/asn1"
	"fmt"
	"net"
	"time"
)

// Transport is an established COTP connection that can send and receive
// session-layer data. The transport/iso subpackage provides production
// implementations; test code typically uses mock transports.
//
// Send transmits the raw session SPDU bytes as COTP user data.
// Receive blocks until data is available or the context is cancelled.
// Close closes the underlying connection.
type Transport interface {
	Send(ctx context.Context, data []byte) error
	Receive(ctx context.Context) ([]byte, error)
	Close() error
}

// TransportListener accepts incoming connections and returns Transport
// instances ready for MMS server use. The production implementation
// lives in the transport/iso subpackage (iso.Listener).
type TransportListener interface {
	Accept(ctx context.Context) (Transport, error)
	Close() error
	Addr() net.Addr
}

// TLSTransport is optionally implemented by [Transport] implementations
// that support TLS. Use a type assertion to check if a transport provides
// TLS connection state:
//
//	if tt, ok := transport.(mms.TLSTransport); ok {
//	    state := tt.TLSConnectionState()
//	}
type TLSTransport interface {
	Transport
	TLSConnectionState() *tls.ConnectionState
}

// RemoteAddrTransport is optionally implemented by [Transport]
// implementations that can report the remote peer's network address.
// Use a type assertion to check:
//
//	if ra, ok := transport.(mms.RemoteAddrTransport); ok {
//	    addr := ra.RemoteAddr()
//	}
type RemoteAddrTransport interface {
	Transport
	RemoteAddr() net.Addr
}

// DomainID identifies an MMS domain (named scope for variables).
type DomainID string

// ItemID identifies a named variable within a domain.
type ItemID string

// InvokeID is a protocol-level request/response correlation identifier.
type InvokeID uint32

// APTitle is an ISO Application Process title (OBJECT IDENTIFIER).
// It is a type alias for [asn1.ObjectIdentifier] so that values can be
// marshaled directly by encoding/asn1 without conversion.
type APTitle = asn1.ObjectIdentifier

// ObjectClass identifies the class of an MMS named object.
type ObjectClass int

const (
	ObjectClassNamedVariable     ObjectClass = 0
	ObjectClassScatteredAccess   ObjectClass = 1
	ObjectClassNamedVariableList ObjectClass = 2
	ObjectClassNamedType         ObjectClass = 3
	ObjectClassSemaphore         ObjectClass = 4
	ObjectClassEventCondition    ObjectClass = 5
	ObjectClassEventAction       ObjectClass = 6
	ObjectClassEventEnrollment   ObjectClass = 7
	ObjectClassJournal           ObjectClass = 8
	ObjectClassDomain            ObjectClass = 9
	ObjectClassProgramInvocation ObjectClass = 10
	ObjectClassOperatorStation   ObjectClass = 11
)

var objectClassNames = [...]string{
	"NamedVariable",
	"ScatteredAccess",
	"NamedVariableList",
	"NamedType",
	"Semaphore",
	"EventCondition",
	"EventAction",
	"EventEnrollment",
	"Journal",
	"Domain",
	"ProgramInvocation",
	"OperatorStation",
}

// String returns the human-readable name of the ObjectClass.
func (c ObjectClass) String() string {
	if int(c) >= 0 && int(c) < len(objectClassNames) {
		return objectClassNames[c]
	}
	return fmt.Sprintf("ObjectClass(%d)", int(c))
}

// ValueType identifies the type of data held in a [Value].
type ValueType int

const (
	ValueTypeBoolean          ValueType = iota
	ValueTypeInteger                    // signed integer
	ValueTypeUnsigned                   // unsigned integer
	ValueTypeFloat                      // IEEE 754 floating point
	ValueTypeBitString                  // ordered sequence of bits
	ValueTypeOctetString                // arbitrary byte sequence
	ValueTypeVisibleString              // ISO 646 visible string
	ValueTypeMmsString                  // MMS string (UTF-8)
	ValueTypeUTCTime                    // UTC timestamp
	ValueTypeBinaryTime                 // binary-encoded time
	ValueTypeArray                      // ordered homogeneous collection
	ValueTypeStructure                  // ordered heterogeneous collection
	ValueTypeDataAccessError            // per-variable access error
	ValueTypeNamedType                  // reference to a named type alias (see TypeSpec.TypeName)
	ValueTypeGeneralizedTime            // ISO 8601 generalized time
	ValueTypeBCD                        // binary-coded decimal
	ValueTypeObjectIdentifier           // ASN.1 OBJECT IDENTIFIER
)

var valueTypeNames = [...]string{
	"Boolean",
	"Integer",
	"Unsigned",
	"Float",
	"BitString",
	"OctetString",
	"VisibleString",
	"MmsString",
	"UTCTime",
	"BinaryTime",
	"Array",
	"Structure",
	"DataAccessError",
	"NamedType",
	"GeneralizedTime",
	"BCD",
	"ObjectIdentifier",
}

// String returns the human-readable name of the ValueType.
func (t ValueType) String() string {
	if int(t) >= 0 && int(t) < len(valueTypeNames) {
		return valueTypeNames[t]
	}
	return fmt.Sprintf("ValueType(%d)", int(t))
}

// DataAccessErrorCode represents per-variable access errors returned
// by MMS read/write operations.
type DataAccessErrorCode int

const (
	DataAccessErrorNone                    DataAccessErrorCode = 0
	DataAccessErrorObjectInvalidated       DataAccessErrorCode = 1
	DataAccessErrorHardwareFault           DataAccessErrorCode = 2
	DataAccessErrorTemporarilyUnavailable  DataAccessErrorCode = 3
	DataAccessErrorObjectAccessDenied      DataAccessErrorCode = 4
	DataAccessErrorObjectUndefined         DataAccessErrorCode = 5
	DataAccessErrorInvalidAddress          DataAccessErrorCode = 6
	DataAccessErrorTypeMismatch            DataAccessErrorCode = 7
	DataAccessErrorTypeInconsistent        DataAccessErrorCode = 8
	DataAccessErrorObjectExists            DataAccessErrorCode = 9
	DataAccessErrorObjectAccessUnsupported DataAccessErrorCode = 10
)

const (
	// Deprecated: Use [DataAccessErrorTemporarilyUnavailable].
	DataAccessErrorTemporarilyUnavail = DataAccessErrorTemporarilyUnavailable

	// Deprecated: Use [DataAccessErrorObjectAccessUnsupported].
	DataAccessErrorObjectAccessUnsup = DataAccessErrorObjectAccessUnsupported
)

var dataAccessErrorNames = [...]string{
	"None",
	"ObjectInvalidated",
	"HardwareFault",
	"TemporarilyUnavailable",
	"ObjectAccessDenied",
	"ObjectUndefined",
	"InvalidAddress",
	"TypeMismatch",
	"TypeInconsistent",
	"ObjectExists",
	"ObjectAccessUnsupported",
}

// String returns the human-readable name of the DataAccessErrorCode.
func (c DataAccessErrorCode) String() string {
	if int(c) >= 0 && int(c) < len(dataAccessErrorNames) {
		return dataAccessErrorNames[c]
	}
	return fmt.Sprintf("DataAccessErrorCode(%d)", int(c))
}

// ErrorClass categorizes MMS service errors from ConfirmedErrorPDU responses.
type ErrorClass int

const (
	ErrorClassVMDState       ErrorClass = 0
	ErrorClassAppReference   ErrorClass = 1
	ErrorClassDefinition     ErrorClass = 2
	ErrorClassResource       ErrorClass = 3
	ErrorClassService        ErrorClass = 4
	ErrorClassServicePreempt ErrorClass = 5
	ErrorClassTimeResolution ErrorClass = 6
	ErrorClassAccess         ErrorClass = 7
	ErrorClassInitiate       ErrorClass = 8
	ErrorClassConclude       ErrorClass = 9
	ErrorClassCancel         ErrorClass = 10
	ErrorClassFile           ErrorClass = 11
	ErrorClassOthers         ErrorClass = 12
)

var errorClassNames = [...]string{
	"VMDState",
	"ApplicationReference",
	"Definition",
	"Resource",
	"Service",
	"ServicePreempt",
	"TimeResolution",
	"Access",
	"Initiate",
	"Conclude",
	"Cancel",
	"File",
	"Others",
}

// String returns the human-readable name of the ErrorClass.
func (c ErrorClass) String() string {
	if int(c) >= 0 && int(c) < len(errorClassNames) {
		return errorClassNames[c]
	}
	return fmt.Sprintf("ErrorClass(%d)", int(c))
}

// VMDLogicalStatus represents the logical status of a VMD (Virtual
// Manufacturing Device).
type VMDLogicalStatus int

const (
	VMDLogicalStatusStateChangesAllowed VMDLogicalStatus = 0
	VMDLogicalStatusNoStateChanges      VMDLogicalStatus = 1
	VMDLogicalStatusLimited             VMDLogicalStatus = 2
)

var vmdLogicalStatusNames = [...]string{
	"StateChangesAllowed",
	"NoStateChanges",
	"Limited",
}

// String returns the human-readable name of the VMDLogicalStatus.
func (s VMDLogicalStatus) String() string {
	if int(s) >= 0 && int(s) < len(vmdLogicalStatusNames) {
		return vmdLogicalStatusNames[s]
	}
	return fmt.Sprintf("VMDLogicalStatus(%d)", int(s))
}

// VMDPhysicalStatus represents the physical status of a VMD.
type VMDPhysicalStatus int

const (
	VMDPhysicalStatusOperational          VMDPhysicalStatus = 0
	VMDPhysicalStatusPartiallyOperational VMDPhysicalStatus = 1
	VMDPhysicalStatusInoperable           VMDPhysicalStatus = 2
	VMDPhysicalStatusNeedsCommissioning   VMDPhysicalStatus = 3
)

// Deprecated: Use [VMDPhysicalStatusPartiallyOperational].
const VMDPhysicalStatusPartiallyOper = VMDPhysicalStatusPartiallyOperational

var vmdPhysicalStatusNames = [...]string{
	"Operational",
	"PartiallyOperational",
	"Inoperable",
	"NeedsCommissioning",
}

// String returns the human-readable name of the VMDPhysicalStatus.
func (s VMDPhysicalStatus) String() string {
	if int(s) >= 0 && int(s) < len(vmdPhysicalStatusNames) {
		return vmdPhysicalStatusNames[s]
	}
	return fmt.Sprintf("VMDPhysicalStatus(%d)", int(s))
}

// ObjectScope specifies the naming scope for MMS operations.
type ObjectScope int

const (
	// ObjectScopeVMD selects VMD-specific scope (global).
	ObjectScopeVMD ObjectScope = iota
	// ObjectScopeDomain selects domain-specific scope (requires a DomainID).
	ObjectScopeDomain
	// ObjectScopeAssociation selects objects scoped to a single
	// association. Association-scope storage is supported, but
	// server-side listing and lifecycle management are not yet
	// implemented.
	ObjectScopeAssociation
)

var objectScopeNames = [...]string{
	"VMD",
	"Domain",
	"Association",
}

// String returns the human-readable name of the ObjectScope.
func (s ObjectScope) String() string {
	if int(s) >= 0 && int(s) < len(objectScopeNames) {
		return objectScopeNames[s]
	}
	return fmt.Sprintf("ObjectScope(%d)", int(s))
}

// ObjectName identifies a named MMS object within a specific scope.
//
// For domain-specific names (the most common case), set Scope to
// [ObjectScopeDomain] with both Domain and ItemID. For VMD-specific
// names, set Scope to [ObjectScopeVMD] and only ItemID. For
// association-specific names, set Scope to [ObjectScopeAssociation]
// and only ItemID.
//
// Note: Scope defaults to [ObjectScopeVMD] (zero value). When
// constructing domain-specific names, always set Scope explicitly.
type ObjectName struct {
	Scope  ObjectScope
	Domain DomainID
	ItemID ItemID
}

// AccessSelector selects a sub-element of a variable for alternate access.
// Exactly one of Component, Index, or IndexRange must be set.
type AccessSelector struct {
	// Component selects a structure member by name.
	Component string

	// Index selects a single array element by zero-based index.
	Index *int

	// IndexRange selects a contiguous range of array elements.
	IndexRange *IndexRange
}

// IndexRange specifies a contiguous range of array elements.
type IndexRange struct {
	Start int // zero-based start index
	Count int // number of elements to select
}

// SelectComponent returns an [AccessSelector] that selects a structure
// member by name.
func SelectComponent(name string) AccessSelector {
	return AccessSelector{Component: name}
}

// SelectIndex returns an [AccessSelector] that selects a single array
// element by zero-based index.
func SelectIndex(i int) AccessSelector {
	return AccessSelector{Index: &i}
}

// SelectRange returns an [AccessSelector] that selects a contiguous
// range of array elements starting at low with count elements.
func SelectRange(low, count int) AccessSelector {
	return AccessSelector{IndexRange: &IndexRange{Start: low, Count: count}}
}

// VariableSpec identifies a variable with optional sub-element selection
// via alternate access. Use this with [Client.ReadVariables] and
// [Client.WriteVariables] for component, index, or range access.
type VariableSpec struct {
	Name            ObjectName
	AlternateAccess []AccessSelector
}

// TypeSpecElement represents a single element within a structure type specification.
type TypeSpecElement struct {
	Name string
	Type TypeSpec
}

// TypeSpec describes the MMS type of a variable. It is returned by
// [Client.GetVariableAccessAttributes] and used to register variables
// with [Server.RegisterVariable].
//
// For scalar types, set Type and optionally Size (bit width for integers,
// length constraint for strings). For structures, populate Elements.
// For arrays, set Count and Element. For named type references, set
// Type to [ValueTypeNamedType] and TypeName to the referenced name.
//
// Introspection helpers: [TypeSpec.ChildByName] and [TypeSpec.ChildByIndex]
// navigate into structures and arrays. [TypeSpec.Resolve] traverses a
// chain of [AccessSelector] values. [TypeSpec.DefaultValue] creates a
// zero-valued [Value] matching the type. [TypeSpec.ShallowCompatible]
// performs a quick pre-flight type check against a Value.
type TypeSpec struct {
	Type     ValueType
	Elements []TypeSpecElement // for Structure
	Count    int               // for Array: element count
	Element  *TypeSpec         // for Array: element type
	Size     int               // for Integer/Unsigned: bit width; for BitString/OctetString/VisibleString/MmsString: size

	// Float-specific fields (only valid when Type is ValueTypeFloat).
	FormatWidth   int // total float format width in bits (e.g. 32, 64)
	ExponentWidth int // exponent width in bits (e.g. 8 for IEEE 754 single)

	// TypeName holds the referenced ObjectName when the server returns
	// a typeName reference instead of an inline type definition.
	// Non-nil only when this TypeSpec represents a named type alias.
	TypeName *ObjectName
}

// ChildByName returns the TypeSpec of a structure element by name.
// Returns false if the TypeSpec is not a structure or the name is
// not found.
func (ts *TypeSpec) ChildByName(name string) (*TypeSpec, bool) {
	if ts.Type != ValueTypeStructure {
		return nil, false
	}
	for i := range ts.Elements {
		if ts.Elements[i].Name == name {
			return &ts.Elements[i].Type, true
		}
	}
	return nil, false
}

// ChildByIndex returns the TypeSpec of a structure element or array
// element by index. For structures, the index maps to the element
// order. For arrays, the element type is returned regardless of index
// (as long as the index is within bounds).
func (ts *TypeSpec) ChildByIndex(index int) (*TypeSpec, bool) {
	switch ts.Type {
	case ValueTypeStructure:
		if index < 0 || index >= len(ts.Elements) {
			return nil, false
		}
		return &ts.Elements[index].Type, true
	case ValueTypeArray:
		if index < 0 || index >= ts.Count {
			return nil, false
		}
		return ts.Element, ts.Element != nil
	default:
		return nil, false
	}
}

// ShallowCompatible reports whether v has a top-level type and element
// count that matches ts. It checks only the outermost level — it does not
// recurse into array elements or structure components.
// Use this for quick pre-flight checks; it does not guarantee the value
// is deeply valid against the full type tree.
func (ts *TypeSpec) ShallowCompatible(v *Value) bool {
	if v == nil {
		return false
	}
	if v.Type() != ts.Type {
		return false
	}
	switch ts.Type {
	case ValueTypeStructure:
		elems, ok := v.Structure()
		if !ok {
			return false
		}
		return len(elems) == len(ts.Elements)
	case ValueTypeArray:
		elems, ok := v.ArrayElements()
		if !ok {
			return false
		}
		return len(elems) == ts.Count
	default:
		return true
	}
}

// DefaultValue creates a zero-valued [Value] matching this TypeSpec.
// For structures and arrays, all elements are recursively initialized.
// Returns nil for unsupported or named-type-reference specs.
//
// For arrays, when Element is nil or Count is 0 the method returns an
// empty array (no elements). This is a deliberate best-effort placeholder:
// a non-zero Count with nil Element means the type spec is incomplete
// (the element type is unknown), so no elements can be initialized.
func (ts *TypeSpec) DefaultValue() *Value {
	switch ts.Type {
	case ValueTypeBoolean:
		return NewBoolean(false)
	case ValueTypeInteger:
		return NewInteger(0)
	case ValueTypeUnsigned:
		return NewUnsigned(0)
	case ValueTypeFloat:
		return NewFloat(0)
	case ValueTypeBitString:
		if ts.Size > 0 {
			return NewBitStringWithLength(make([]byte, (ts.Size+7)/8), ts.Size)
		}
		return NewBitString(nil)
	case ValueTypeOctetString:
		return NewOctetString(nil)
	case ValueTypeVisibleString:
		return NewVisibleString("")
	case ValueTypeMmsString:
		return NewMmsString("")
	case ValueTypeUTCTime:
		return NewUTCTime(time.Time{})
	case ValueTypeBinaryTime:
		return NewBinaryTime(0)
	case ValueTypeGeneralizedTime:
		return NewGeneralizedTime(time.Time{})
	case ValueTypeBCD:
		return NewBCD(0)
	case ValueTypeObjectIdentifier:
		return NewObjectIdentifier(nil)
	case ValueTypeStructure:
		elems := make([]*Value, len(ts.Elements))
		for i := range ts.Elements {
			elems[i] = ts.Elements[i].Type.DefaultValue()
			if elems[i] == nil {
				return nil
			}
		}
		return NewStructure(elems)
	case ValueTypeArray:
		if ts.Element == nil || ts.Count == 0 {
			return NewArray(nil)
		}
		elems := make([]*Value, ts.Count)
		for i := range elems {
			elems[i] = ts.Element.DefaultValue()
			if elems[i] == nil {
				return nil
			}
		}
		return NewArray(elems)
	default:
		return nil
	}
}

// Resolve traverses the type tree using a chain of access selectors,
// returning the TypeSpec of the selected sub-element.
func (ts *TypeSpec) Resolve(selectors ...AccessSelector) (*TypeSpec, error) {
	cur := ts
	for i, sel := range selectors {
		if cur == nil {
			return nil, fmt.Errorf("nil TypeSpec at selector [%d]", i)
		}
		switch {
		case sel.Component != "":
			child, ok := cur.ChildByName(sel.Component)
			if !ok {
				return nil, fmt.Errorf("selector [%d]: component %q not found in %s", i, sel.Component, cur.Type)
			}
			cur = child
		case sel.Index != nil:
			child, ok := cur.ChildByIndex(*sel.Index)
			if !ok {
				return nil, fmt.Errorf("selector [%d]: index %d out of range for %s", i, *sel.Index, cur.Type)
			}
			cur = child
		case sel.IndexRange != nil:
			if cur.Type != ValueTypeArray {
				return nil, fmt.Errorf("selector [%d]: index range on non-array type %s", i, cur.Type)
			}
			if cur.Element == nil {
				return nil, fmt.Errorf("selector [%d]: array has no element type", i)
			}
			sub := TypeSpec{
				Type:    ValueTypeArray,
				Count:   sel.IndexRange.Count,
				Element: cur.Element,
			}
			cur = &sub
		default:
			return nil, fmt.Errorf("selector [%d]: no field set", i)
		}
	}
	return cur, nil
}

// ServerIdentity holds the result of an MMS Identify service response.
type ServerIdentity struct {
	Vendor   string
	Model    string
	Revision string
}

// ServerStatus holds the result of an MMS Status service response.
type ServerStatus struct {
	Logical  VMDLogicalStatus
	Physical VMDPhysicalStatus
}

// InformationReportIndication is delivered to the client when the
// server sends an unsolicited InformationReport.
type InformationReportIndication struct {
	// ListName is set when the report references a named variable list.
	// When nil, Variables contains the individual variable specifications.
	ListName *ObjectName

	// Variables lists the individual variable specifications. Empty
	// when the report uses a named variable list (ListName is set).
	Variables []ObjectName

	// Values holds one value per variable (or per list member).
	Values []*Value
}

// InformationReportHandler is called when the client receives an
// unsolicited InformationReport from the server.
//
// The handler is called on the client's internal reader goroutine.
// Long-running work should be dispatched to a separate goroutine.
type InformationReportHandler func(report *InformationReportIndication)

// InformationReportRequest is used to send an InformationReport from the
// server to a connected client.
type InformationReportRequest struct {
	// ListName references a named variable list. When set, Variables
	// should be empty — the receiver uses the list definition.
	ListName *ObjectName

	// Variables lists the individual variable names. Ignored if
	// ListName is set.
	Variables []ObjectName

	// Values holds one value per variable (or per list member).
	Values []*Value
}

// NVLAccessResult extends [AccessResult] with the variable
// specification that produced it, as returned when
// [ReadNamedVariableListOptions.SpecificationWithResult] is true.
type NVLAccessResult struct {
	Variable  *VariableSpec
	Value     *Value
	ErrorCode DataAccessErrorCode
}

// WriteAccessResult holds the per-variable outcome from a multi-variable
// write operation. Success is true when ErrorCode is zero.
type WriteAccessResult struct {
	Index     int
	Success   bool
	ErrorCode DataAccessErrorCode
}

// NegotiatedParameters holds the MMS parameters negotiated during
// association establishment. Access via [Client.Negotiated].
type NegotiatedParameters struct {
	MaxPDUSize    int
	MaxOutCalling int
	MaxOutCalled  int
	NestingLevel  int
	ServerVersion int
}

// FileOpenOptions configures a [Client.FileOpen] call. The zero value
// opens the file at position 0 (start of file).
type FileOpenOptions struct {
	// InitialPosition is the byte offset at which to begin reading.
	InitialPosition uint32
}

// FileDirectoryRequest specifies the scope and pagination for a
// file directory listing. The zero value lists all files.
type FileDirectoryRequest struct {
	FileSpec      string
	ContinueAfter string
}

// FileDirectoryResult holds the result of a paginated file directory
// listing.
type FileDirectoryResult struct {
	Entries       []FileDirectoryEntry
	MoreFollows   bool
	ContinueAfter string
}

// FileListRequest is the server-side file listing request passed to
// [FileProvider.List]. ContinueAfter is the file name to resume after
// (empty for the first page). MaxEntries limits the number of entries
// returned (0 means provider-default).
type FileListRequest struct {
	FileSpec      string
	ContinueAfter string
	MaxEntries    int
}

// FileListResult is the server-side file listing result returned by
// [FileProvider.List].
type FileListResult struct {
	Entries     []FileEntry
	MoreFollows bool
}

// ReadNamedVariableListOptions configures a [Client.ReadNamedVariableList] call.
type ReadNamedVariableListOptions struct {
	// SpecificationWithResult requests that the server include the
	// variable specification alongside each result value. When false
	// (the default), only values are returned.
	SpecificationWithResult bool
}
