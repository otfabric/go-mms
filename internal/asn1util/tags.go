package asn1util

// MMS PDU top-level CHOICE tags (first byte of the BER encoding).
// These are context-specific, implicitly tagged.
const (
	TagConfirmedRequest  byte = 0xa0 // context 0, constructed
	TagConfirmedResponse byte = 0xa1 // context 1, constructed
	TagConfirmedError    byte = 0xa2 // context 2, constructed
	TagUnconfirmed       byte = 0xa3 // context 3, constructed
	TagReject            byte = 0xa4 // context 4, constructed
	TagInitiateRequest   byte = 0xa8 // context 8, constructed
	TagInitiateResponse  byte = 0xa9 // context 9, constructed
	TagInitiateError     byte = 0xaa // context 10, constructed
	TagConcludeRequest   byte = 0x8b // context 11, primitive (NULL)
	TagConcludeResponse  byte = 0x8c // context 12, primitive (NULL)
	TagConcludeError     byte = 0x8d // context 13, primitive
)

// ConfirmedServiceRequest CHOICE tags within a ConfirmedRequestPdu.
const (
	TagServiceGetNameList                 byte = 0xa1 // context 1, constructed
	TagServiceIdentify                    byte = 0x82 // context 2, primitive (empty)
	TagServiceRead                        byte = 0xa4 // context 4, constructed
	TagServiceWrite                       byte = 0xa5 // context 5, constructed
	TagServiceGetVariableAccessAttributes byte = 0xa6 // context 6, constructed (explicit)
	TagServiceStatus                      byte = 0x80 // context 0, primitive (empty)
	TagServiceDefineNamedVariableList     byte = 0xab // context 11, constructed
	TagServiceGetNamedVariableListAttrs   byte = 0xac // context 12, constructed (explicit)
	TagServiceDeleteNamedVariableList     byte = 0xad // context 13, constructed
)

// ConfirmedServiceResponse CHOICE tags within a ConfirmedResponsePdu.
const (
	TagRespGetNameList                 byte = 0xa1
	TagRespIdentify                    byte = 0xa2 // context 2, constructed
	TagRespRead                        byte = 0xa4
	TagRespWrite                       byte = 0xa5
	TagRespGetVariableAccessAttributes byte = 0xa6
	TagRespStatus                      byte = 0xa0 // context 0, constructed
	TagRespDefineNamedVariableList     byte = 0xab
	TagRespGetNamedVariableListAttrs   byte = 0xac
	TagRespDeleteNamedVariableList     byte = 0xad
)

// Service CHOICE tag numbers within ConfirmedServiceRequest /
// ConfirmedServiceResponse. These are the integer tag numbers, not
// the raw single-byte tag values. For tags 0–30, the number is
// derived via TagNumber(); for tags >30 (file services) multi-byte
// BER encoding is used on the wire.
const (
	TagNumStatus                      = 0  // 0x00
	TagNumGetNameList                 = 1  // 0x01
	TagNumIdentify                    = 2  // 0x02
	TagNumRead                        = 4  // 0x04
	TagNumWrite                       = 5  // 0x05
	TagNumGetVariableAccessAttributes = 6  // 0x06
	TagNumDefineNamedVariableList     = 11 // 0x0b
	TagNumGetNamedVariableListAttrs   = 12 // 0x0c
	TagNumDeleteNamedVariableList     = 13 // 0x0d

	TagNumObtainFile = 46 // 0x2e

	TagNumReadJournal = 65 // 0x41

	TagNumFileOpen      = 72 // 0x48
	TagNumFileRead      = 73 // 0x49
	TagNumFileClose     = 74 // 0x4a
	TagNumFileRename    = 75 // 0x4b
	TagNumFileDelete    = 76 // 0x4c
	TagNumFileDirectory = 77 // 0x4d
)

// BER tag class and construction bits for building tags.
const (
	ClassUniversal   = 0x00
	ClassApplication = 0x40
	ClassContext     = 0x80
	ClassPrivate     = 0xc0
	ConstructedFlag  = 0x20
)

// TagNumber extracts the tag number from a single-byte BER tag.
// Only valid for tag numbers 0–30 (single-byte form).
func TagNumber(tag byte) int {
	return int(tag & 0x1f)
}

// IsConstructed returns true if the tag indicates a constructed encoding.
func IsConstructed(tag byte) bool {
	return tag&ConstructedFlag != 0
}

// TagClass returns the class bits from a BER tag byte.
func TagClass(tag byte) byte {
	return tag & 0xc0
}
