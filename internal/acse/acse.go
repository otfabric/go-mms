// Package acse implements ACSE (Association Control Service Element)
// PDU construction and parsing per ISO 8650 / X.217.
//
// Only the subset needed by MMS is implemented: AARQ, AARE, RLRQ,
// RLRE, and ABRT APDUs.
// This is internal protocol plumbing — users interact with mms.Client.
package acse

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/berutil"
)

// ACSE APDU tags ([APPLICATION n] constructed).
const (
	TagAARQ byte = 0x60 // [APPLICATION 0]
	TagAARE byte = 0x61 // [APPLICATION 1]
	TagRLRQ byte = 0x62 // [APPLICATION 2]
	TagRLRE byte = 0x63 // [APPLICATION 3]
	TagABRT byte = 0x64 // [APPLICATION 4]
)

// appContextMmsEncoded is the BER/DER encoding of the MMS application
// context OID 1.0.9506.2.3 (iso(1) standard(0) 9506 part(2) mms(3)).
// Hardcoded to avoid a package-init panic from asn1.Marshal.
var appContextMmsEncoded = []byte{0x06, 0x05, 0x28, 0xca, 0x22, 0x02, 0x03}

// AssociateResult values in AARE.
const (
	ResultAccepted          = 0
	ResultRejectedPerm      = 1
	ResultRejectedTransient = 2
)

// ApduType classifies a parsed ACSE APDU.
type ApduType byte

const (
	ApduAARQ ApduType = ApduType(TagAARQ)
	ApduAARE ApduType = ApduType(TagAARE)
	ApduRLRQ ApduType = ApduType(TagRLRQ)
	ApduRLRE ApduType = ApduType(TagRLRE)
	ApduABRT ApduType = ApduType(TagABRT)
)

func (t ApduType) String() string {
	switch t {
	case ApduAARQ:
		return "AARQ"
	case ApduAARE:
		return "AARE"
	case ApduRLRQ:
		return "RLRQ"
	case ApduRLRE:
		return "RLRE"
	case ApduABRT:
		return "ABRT"
	default:
		return fmt.Sprintf("ApduType(0x%02x)", byte(t))
	}
}

// AARQParams holds parameters for building an AARQ (Associate Request).
//
// AP-title and AE-qualifier are treated as a pair: if an AP-title is
// provided, the corresponding AE-qualifier is always included. If the
// AP-title is nil/empty, the AE-qualifier is silently omitted
// (an AE-qualifier without an AP-title has no meaning per ISO 8650).
type AARQParams struct {
	CalledAPTitle      asn1.ObjectIdentifier
	CalledAEQualifier  int
	CallingAPTitle     asn1.ObjectIdentifier
	CallingAEQualifier int
	Password           []byte // if non-nil, includes ACSE password authentication
}

// AuthMechanism identifies the ACSE authentication mechanism parsed
// from an AARQ. Only protocol-level mechanisms are represented here;
// transport-level mechanisms (TLS) are classified at the server layer.
type AuthMechanism int

const (
	AuthNone     AuthMechanism = iota // no authentication fields in AARQ
	AuthPassword                      // ACSE password (OID 2.2.3.1)
	AuthUnknown                       // unknown/unsupported mechanism OID
)

// authMechPasswordOID is the BER-encoded OID 2.2.3.1 used in the AARQ
// to indicate password-based ACSE authentication.
var authMechPasswordOID = []byte{0x52, 0x03, 0x01}

// tagAuthValueGraphicString is the CHOICE tag for graphicString [0]
// inside authentication-value, used for password auth.
const tagAuthValueGraphicString byte = 0x80

// AuthInfo holds the authentication and identity information extracted
// from an AARQ.
type AuthInfo struct {
	Mechanism    AuthMechanism
	MechanismOID asn1.ObjectIdentifier // decoded mechanism OID; nil when AuthNone
	Password     []byte                // populated when Mechanism == AuthPassword

	CallingAPTitle     asn1.ObjectIdentifier // calling AP-title; nil when absent
	CallingAEQualifier *int                  // calling AE-qualifier; nil when absent
}

// ParsedAARE holds the parsed result of an AARE (Associate Response).
type ParsedAARE struct {
	Result   int    // 0=accepted, 1=rejected-permanent, 2=rejected-transient
	UserData []byte // MMS Initiate Response payload
}

// ParsedAPDU holds the result of parsing any ACSE APDU.
type ParsedAPDU struct {
	Type     ApduType
	AARE     *ParsedAARE // populated only when Type == ApduAARE
	Auth     AuthInfo    // populated for AARQ: authentication mechanism + value
	UserData []byte      // for AARQ: MMS payload; for RLRQ/RLRE: nil
}

// EncodeAARQ builds an AARQ APDU (Associate Request) containing
// the application context, AP-titles, AE-qualifiers, and the given
// MMS payload (typically MMS Initiate Request) as user-information.
func EncodeAARQ(p AARQParams, mmsPayload []byte) ([]byte, error) {
	var content []byte

	// [1] application-context-name
	content = berutil.AppendTLV(content, 0xa1, appContextMmsEncoded)

	// [2] called-AP-title + [3] called-AE-qualifier (paired)
	if len(p.CalledAPTitle) > 0 {
		oid, err := marshalOID(p.CalledAPTitle)
		if err != nil {
			return nil, fmt.Errorf("acse: marshal called AP-title: %w", err)
		}
		content = berutil.AppendTLV(content, 0xa2, oid)
		content = berutil.AppendTLV(content, 0xa3, encodeAEQualifier(p.CalledAEQualifier))
	}

	// [6] calling-AP-title + [7] calling-AE-qualifier (paired)
	if len(p.CallingAPTitle) > 0 {
		oid, err := marshalOID(p.CallingAPTitle)
		if err != nil {
			return nil, fmt.Errorf("acse: marshal calling AP-title: %w", err)
		}
		content = berutil.AppendTLV(content, 0xa6, oid)
		content = berutil.AppendTLV(content, 0xa7, encodeAEQualifier(p.CallingAEQualifier))
	}

	// ACSE authentication fields
	if len(p.Password) > 0 {
		// [10] sender-acse-requirements: authentication functional unit
		content = berutil.AppendTLV(content, 0x8a, []byte{0x04, 0x80})

		// [11] mechanism-name: password OID 2.2.3.1
		content = berutil.AppendTLV(content, 0x8b, authMechPasswordOID)

		// [12] authentication-value: graphicString [0]
		authVal := berutil.EncodeTLV(0x80, p.Password)
		content = berutil.AppendTLV(content, 0xac, authVal)
	}

	// [30] user-information (EXTERNAL containing MMS payload)
	if len(mmsPayload) > 0 {
		content = append(content, encodeUserInformation(mmsPayload)...)
	}

	return berutil.EncodeTLV(TagAARQ, content), nil
}

// EncodeAARE builds an AARE APDU (Associate Response).
func EncodeAARE(result int, mmsPayload []byte) []byte {
	var content []byte

	// [1] application-context-name
	content = berutil.AppendTLV(content, 0xa1, appContextMmsEncoded)

	// [2] result
	resultEnc := berutil.EncodeTLV(0x02, []byte{byte(result)})
	content = berutil.AppendTLV(content, 0xa2, resultEnc)

	// [3] result-source-diagnostic
	diag := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x02, []byte{0x00}))
	content = berutil.AppendTLV(content, 0xa3, diag)

	// [30] user-information
	if len(mmsPayload) > 0 {
		content = append(content, encodeUserInformation(mmsPayload)...)
	}

	return berutil.EncodeTLV(TagAARE, content)
}

// EncodeRLRQ builds a RLRQ APDU (Release Request): reason=normal.
func EncodeRLRQ() []byte {
	return berutil.EncodeTLV(TagRLRQ, []byte{0x80, 0x01, 0x00})
}

// EncodeRLRE builds a RLRE APDU (Release Response): empty.
func EncodeRLRE() []byte {
	return []byte{TagRLRE, 0x00}
}

// EncodeABRT builds an ABRT APDU with the given source (0=user, 1=provider).
func EncodeABRT(source int) []byte {
	return berutil.EncodeTLV(TagABRT, []byte{0x80, 0x01, byte(source)})
}

// Parse parses an ACSE APDU from raw bytes.
func Parse(data []byte) (*ParsedAPDU, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("acse: APDU too short (%d bytes)", len(data))
	}

	tag := data[0]
	_, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("acse: APDU outer: %w", err)
	}

	result := &ParsedAPDU{Type: ApduType(tag)}

	switch tag {
	case TagAARQ:
		result.UserData, result.Auth, err = parseAARQ(content)
		if err != nil {
			return nil, err
		}
	case TagAARE:
		aare, err := parseAARE(content)
		if err != nil {
			return nil, err
		}
		result.AARE = aare
		result.UserData = aare.UserData
	case TagRLRQ:
		if err := validateRLRQ(content); err != nil {
			return nil, err
		}
	case TagRLRE:
		// RLRE may be empty — nothing to validate beyond the outer TLV
	case TagABRT:
		if err := validateABRT(content); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("acse: unknown APDU tag 0x%02x", tag)
	}

	return result, nil
}

func parseAARE(content []byte) (*ParsedAARE, error) {
	aare := &ParsedAARE{Result: -1}

	offset := 0
	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("acse: AARE field: %w", err)
		}
		offset += n

		switch tag {
		case 0xa2: // result
			_, resultInner, err := berutil.DecodeTLV(inner)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE result: %w", err)
			}
			val, err := berutil.DecodeInteger(resultInner)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE result value: %w", err)
			}
			aare.Result = val
		case 0xbe: // user-information
			userData, err := parseExternalPayload(inner)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE user-information: %w", err)
			}
			aare.UserData = userData
		default:
			// Interop: skip unknown optional ACSE fields. ISO 8650 defines many
			// optional fields (application-context-name, AP/AE qualifiers, etc.)
			// that we do not process. Rejecting them would break interop.
		}
	}

	if aare.Result == -1 {
		return nil, fmt.Errorf("acse: AARE missing required result field")
	}
	if aare.Result < ResultAccepted || aare.Result > ResultRejectedTransient {
		return nil, fmt.Errorf("acse: AARE invalid result value %d", aare.Result)
	}

	return aare, nil
}

func parseAARQ(content []byte) (userData []byte, auth AuthInfo, err error) {
	var mechOIDRaw []byte
	var rawAuthValue []byte
	hasMechanism := false
	hasAuthValue := false

	offset := 0
	for offset < len(content) {
		tag, inner, n, parseErr := berutil.DecodeTLVAt(content, offset)
		if parseErr != nil {
			return nil, AuthInfo{}, fmt.Errorf("acse: AARQ field: %w", parseErr)
		}
		offset += n

		switch tag {
		case 0xa6: // [6] calling-AP-title (EXPLICIT, contains OID)
			var oid asn1.ObjectIdentifier
			if _, oidErr := asn1.Unmarshal(inner, &oid); oidErr != nil {
				return nil, AuthInfo{}, fmt.Errorf("acse: AARQ calling-AP-title: %w", oidErr)
			}
			auth.CallingAPTitle = oid
		case 0xa7: // [7] calling-AE-qualifier (EXPLICIT, contains INTEGER)
			var val int
			if _, oidErr := asn1.Unmarshal(inner, &val); oidErr != nil {
				return nil, AuthInfo{}, fmt.Errorf("acse: AARQ calling-AE-qualifier: %w", oidErr)
			}
			auth.CallingAEQualifier = &val
		case 0x8b: // mechanism-name (OID, implicit)
			mechOIDRaw = inner
			hasMechanism = true
		case 0xac: // authentication-value (CHOICE, [12] explicit)
			rawAuthValue = inner
			hasAuthValue = true
		case 0xbe: // user-information
			userData, err = parseExternalPayload(inner)
			if err != nil {
				return nil, AuthInfo{}, err
			}
		default:
			// Interop: skip unknown optional ACSE fields. ISO 8650 defines many
			// optional fields (application-context-name, AP/AE qualifiers, etc.)
			// that we do not process. Rejecting them would break interop.
		}
	}

	// Strict validation of auth field pairing.
	if hasAuthValue && !hasMechanism {
		return nil, AuthInfo{}, fmt.Errorf("acse: AARQ authentication-value without mechanism-name")
	}

	if hasMechanism {
		auth.MechanismOID, err = decodeImplicitOID(mechOIDRaw)
		if err != nil {
			return nil, AuthInfo{}, fmt.Errorf("acse: AARQ mechanism OID: %w", err)
		}
		auth.Mechanism = classifyMechanism(mechOIDRaw)

		switch auth.Mechanism {
		case AuthPassword:
			if !hasAuthValue {
				return nil, AuthInfo{}, fmt.Errorf("acse: AARQ password mechanism without authentication-value")
			}
			pw, parseErr := parsePasswordAuthValue(rawAuthValue)
			if parseErr != nil {
				return nil, AuthInfo{}, parseErr
			}
			auth.Password = pw
		default:
		}
	}

	return userData, auth, nil
}

// decodeImplicitOID decodes raw OID value bytes (from an implicitly
// tagged field) into an asn1.ObjectIdentifier.
func decodeImplicitOID(raw []byte) (asn1.ObjectIdentifier, error) {
	der := berutil.EncodeTLV(0x06, raw)
	var oid asn1.ObjectIdentifier
	_, err := asn1.Unmarshal(der, &oid)
	return oid, err
}

func classifyMechanism(oid []byte) AuthMechanism {
	if len(oid) == len(authMechPasswordOID) {
		match := true
		for i := range oid {
			if oid[i] != authMechPasswordOID[i] {
				match = false
				break
			}
		}
		if match {
			return AuthPassword
		}
	}
	return AuthUnknown
}

// parsePasswordAuthValue strictly parses the authentication-value for
// password auth. Expects a single graphicString [0] CHOICE and rejects
// malformed or trailing data.
func parsePasswordAuthValue(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("acse: empty authentication-value")
	}

	tag, inner, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return nil, fmt.Errorf("acse: malformed authentication-value: %w", err)
	}
	if tag != tagAuthValueGraphicString {
		return nil, fmt.Errorf("acse: authentication-value: expected graphicString tag 0x%02x, got 0x%02x",
			tagAuthValueGraphicString, tag)
	}
	if n != len(data) {
		return nil, fmt.Errorf("acse: authentication-value: %d trailing bytes after graphicString", len(data)-n)
	}

	return inner, nil
}

// parseExternalPayload extracts the MMS payload from EXTERNAL encoding:
// 0x28 (EXTERNAL) → 0x02 (indirect-reference) → 0xa0 (single-ASN1-type).
func parseExternalPayload(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("acse: empty user-information")
	}

	tag, extContent, err := berutil.DecodeTLV(data)
	if err != nil {
		return nil, fmt.Errorf("acse: EXTERNAL: %w", err)
	}
	if tag != 0x28 {
		return nil, fmt.Errorf("acse: expected EXTERNAL (0x28), got 0x%02x", tag)
	}

	var payload []byte
	offset := 0
	for offset < len(extContent) {
		tag, inner, n, err := berutil.DecodeTLVAt(extContent, offset)
		if err != nil {
			return nil, fmt.Errorf("acse: EXTERNAL field: %w", err)
		}
		offset += n

		if tag == 0xa0 { // single-ASN1-type encoding
			payload = inner
		}
	}

	if payload == nil {
		return nil, fmt.Errorf("acse: EXTERNAL missing single-ASN1-type (0xa0) encoding")
	}
	return payload, nil
}

func validateRLRQ(content []byte) error {
	if len(content) == 0 {
		return nil // RLRQ with no content is tolerated
	}
	tag, _, n, err := berutil.DecodeTLVAt(content, 0)
	if err != nil {
		return fmt.Errorf("acse: RLRQ field: %w", err)
	}
	if tag != 0x80 {
		return fmt.Errorf("acse: RLRQ unexpected field tag 0x%02x, expected 0x80 (reason)", tag)
	}
	if n != len(content) {
		return fmt.Errorf("acse: RLRQ %d trailing bytes after reason", len(content)-n)
	}
	return nil
}

func validateABRT(content []byte) error {
	if len(content) == 0 {
		return fmt.Errorf("acse: ABRT missing abort-source")
	}
	tag, _, n, err := berutil.DecodeTLVAt(content, 0)
	if err != nil {
		return fmt.Errorf("acse: ABRT field: %w", err)
	}
	if tag != 0x80 {
		return fmt.Errorf("acse: ABRT unexpected field tag 0x%02x, expected 0x80 (abort-source)", tag)
	}
	if n != len(content) {
		return fmt.Errorf("acse: ABRT %d trailing bytes after abort-source", len(content)-n)
	}
	return nil
}

func encodeUserInformation(payload []byte) []byte {
	indirectRefTLV := berutil.EncodeTLV(0x02, berutil.EncodeInt(3))
	singleASN1 := berutil.EncodeTLV(0xa0, payload)

	extContent := make([]byte, 0, len(indirectRefTLV)+len(singleASN1))
	extContent = append(extContent, indirectRefTLV...)
	extContent = append(extContent, singleASN1...)
	external := berutil.EncodeTLV(0x28, extContent)

	return berutil.EncodeTLV(0xbe, external)
}

func encodeAEQualifier(val int) []byte {
	return berutil.EncodeTLV(0x02, berutil.EncodeInt(val))
}

func marshalOID(oid asn1.ObjectIdentifier) ([]byte, error) {
	b, err := asn1.Marshal(oid)
	if err != nil {
		return nil, err
	}
	return b, nil
}
