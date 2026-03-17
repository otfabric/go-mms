package mms

import "fmt"

// DomainID identifies an MMS domain (named scope for variables).
type DomainID string

// ItemID identifies a named variable within a domain.
type ItemID string

// InvokeID is a protocol-level request/response correlation identifier.
type InvokeID uint32

// APTitle represents an ISO Application Process title as an OID arc sequence.
type APTitle []int

// ObjectClass identifies the class of an MMS named object.
type ObjectClass int

const (
	ObjectClassNamedVariable     ObjectClass = 0
	ObjectClassScatteredAccess   ObjectClass = 1
	ObjectClassNamedVariableList ObjectClass = 2
	ObjectClassJournal           ObjectClass = 3
	ObjectClassSemaphore         ObjectClass = 4
	ObjectClassEventCondition    ObjectClass = 5
	ObjectClassEventAction       ObjectClass = 6
	ObjectClassEventEnrollment   ObjectClass = 7
	ObjectClassDomain            ObjectClass = 8
	ObjectClassProgramInvocation ObjectClass = 9
	ObjectClassOperatorStation   ObjectClass = 10
)

var objectClassNames = [...]string{
	"NamedVariable",
	"ScatteredAccess",
	"NamedVariableList",
	"Journal",
	"Semaphore",
	"EventCondition",
	"EventAction",
	"EventEnrollment",
	"Domain",
	"ProgramInvocation",
	"OperatorStation",
}

func (c ObjectClass) String() string {
	if int(c) >= 0 && int(c) < len(objectClassNames) {
		return objectClassNames[c]
	}
	return fmt.Sprintf("ObjectClass(%d)", int(c))
}

// ValueType identifies the type of data held in a [Value].
type ValueType int

const (
	ValueTypeBoolean        ValueType = iota
	ValueTypeInteger                  // signed integer
	ValueTypeUnsigned                 // unsigned integer
	ValueTypeFloat                    // IEEE 754 floating point
	ValueTypeBitString                // ordered sequence of bits
	ValueTypeOctetString              // arbitrary byte sequence
	ValueTypeVisibleString            // ISO 646 visible string
	ValueTypeMmsString                // MMS string (UTF-8)
	ValueTypeUTCTime                  // UTC timestamp
	ValueTypeBinaryTime               // binary-encoded time
	ValueTypeArray                    // ordered homogeneous collection
	ValueTypeStructure                // ordered heterogeneous collection
	ValueTypeDataAccessError          // per-variable access error
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
}

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
	DataAccessErrorNone               DataAccessErrorCode = 0
	DataAccessErrorObjectInvalidated  DataAccessErrorCode = 1
	DataAccessErrorHardwareFault      DataAccessErrorCode = 2
	DataAccessErrorTemporarilyUnavail DataAccessErrorCode = 3
	DataAccessErrorObjectAccessDenied DataAccessErrorCode = 4
	DataAccessErrorObjectUndefined    DataAccessErrorCode = 5
	DataAccessErrorInvalidAddress     DataAccessErrorCode = 6
	DataAccessErrorTypeMismatch       DataAccessErrorCode = 7
	DataAccessErrorTypeInconsistent   DataAccessErrorCode = 8
	DataAccessErrorObjectExists       DataAccessErrorCode = 9
	DataAccessErrorObjectAccessUnsup  DataAccessErrorCode = 10
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

func (c DataAccessErrorCode) String() string {
	if int(c) >= 0 && int(c) < len(dataAccessErrorNames) {
		return dataAccessErrorNames[c]
	}
	return fmt.Sprintf("DataAccessErrorCode(%d)", int(c))
}

// ErrorClass categorizes MMS service errors from ConfirmedErrorPDU responses.
type ErrorClass int

const (
	ErrorClassVMDState        ErrorClass = 0
	ErrorClassAppReference    ErrorClass = 1
	ErrorClassDefinition      ErrorClass = 2
	ErrorClassResource        ErrorClass = 3
	ErrorClassService         ErrorClass = 4
	ErrorClassServicePreempt  ErrorClass = 5
	ErrorClassTimeResolution  ErrorClass = 6
	ErrorClassAccess          ErrorClass = 7
	ErrorClassInitiate        ErrorClass = 8
	ErrorClassConclude        ErrorClass = 9
	ErrorClassCancel          ErrorClass = 10
	ErrorClassFile            ErrorClass = 11
	ErrorClassOthers          ErrorClass = 12
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

func (s VMDLogicalStatus) String() string {
	if int(s) >= 0 && int(s) < len(vmdLogicalStatusNames) {
		return vmdLogicalStatusNames[s]
	}
	return fmt.Sprintf("VMDLogicalStatus(%d)", int(s))
}

// VMDPhysicalStatus represents the physical status of a VMD.
type VMDPhysicalStatus int

const (
	VMDPhysicalStatusOperational    VMDPhysicalStatus = 0
	VMDPhysicalStatusPartiallyOper  VMDPhysicalStatus = 1
	VMDPhysicalStatusInoperable     VMDPhysicalStatus = 2
	VMDPhysicalStatusNeedsCommision VMDPhysicalStatus = 3
)

var vmdPhysicalStatusNames = [...]string{
	"Operational",
	"PartiallyOperational",
	"Inoperable",
	"NeedsCommissioning",
}

func (s VMDPhysicalStatus) String() string {
	if int(s) >= 0 && int(s) < len(vmdPhysicalStatusNames) {
		return vmdPhysicalStatusNames[s]
	}
	return fmt.Sprintf("VMDPhysicalStatus(%d)", int(s))
}

// ObjectName identifies a named MMS object within a specific scope.
type ObjectName struct {
	Domain DomainID
	ItemID ItemID
}

// TypeSpecElement represents a single element within a structure type specification.
type TypeSpecElement struct {
	Name string
	Type TypeSpec
}

// TypeSpec describes the MMS type of a variable.
type TypeSpec struct {
	Type     ValueType
	Elements []TypeSpecElement // for Structure
	Count    int               // for Array: element count
	Element  *TypeSpec         // for Array: element type
	Size     int               // for Integer/Unsigned: bit width; for strings: max length
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
