// SPDX-License-Identifier: MIT

// Package acse implements ACSE (Association Control Service Element)
// PDU construction and parsing per ISO 8650 / X.217.
//
// Only the subset needed by MMS is implemented: AARQ, AARE, RLRQ,
// RLRE, and ABRT APDUs.
// This is internal protocol plumbing — users interact with mms.Client.
package acse

import (
	"bytes"
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

// Associate-source-diagnostic CHOICE alternatives (ISO 8650-1 AARE
// result-source-diagnostic [3]).
const (
	DiagSourceUser     = 1 // acse-service-user [1]
	DiagSourceProvider = 2 // acse-service-provider [2]
)

// acse-service-user diagnostic values (ISO 8650-1).
const (
	DiagUserNull                        = 0
	DiagUserNoReasonGiven               = 1
	DiagUserAppContextNotSupported      = 2
	DiagUserCallingAPTitleNotRecognized = 3
	DiagUserAuthMechNameNotRecognized   = 11
	DiagUserAuthMechNameRequired        = 12
	DiagUserAuthFailure                 = 13
	DiagUserAuthRequired                = 14
)

// acse-service-provider diagnostic values (ISO 8650-1).
const (
	DiagProviderNull                = 0
	DiagProviderNoReasonGiven       = 1
	DiagProviderNoCommonACSEVersion = 2
)

// ABRT-source values (ISO 8650-1).
const (
	AbortSourceUser     = 0
	AbortSourceProvider = 1
)

// MMSApplicationContext is the content (without tag and length) of the
// MMS application context name 1.0.9506.2.3 (ISO 9506-2 Annex A.7).
var MMSApplicationContext = []byte{0x28, 0xca, 0x22, 0x02, 0x03}

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

	// MMSContextID is the presentation context id named by the
	// user-information indirect-reference; 0 means 3.
	MMSContextID int
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
	UserData []byte // MMS Initiate Response (or Initiate-Error) payload

	// AppContext is the application-context-name OID content; nil when
	// absent.
	AppContext []byte
	// DiagSource is DiagSourceUser or DiagSourceProvider, 0 when the
	// result-source-diagnostic is absent; Diagnostic is its value.
	DiagSource int
	Diagnostic int
	// ResponderAuth reports the authentication bit of
	// responder-acse-requirements [8].
	ResponderAuth bool
}

// AARQInfo holds the AARQ fields a responder validates.
type AARQInfo struct {
	// ProtocolVersionOK is false when protocol-version [0] is present
	// without the version1 bit (ISO 8650-1: DEFAULT {version1}).
	ProtocolVersionOK bool
	// AppContext is the application-context-name OID content.
	AppContext []byte
	// AuthRequested reports the authentication bit of
	// sender-acse-requirements [10].
	AuthRequested bool
	// CalledAPTitle and CalledAEQualifier are [2] and [3]; nil when absent.
	CalledAPTitle     asn1.ObjectIdentifier
	CalledAEQualifier *int
}

// IsMMS reports whether the AARQ names the MMS application context.
func (i *AARQInfo) IsMMS() bool { return bytes.Equal(i.AppContext, MMSApplicationContext) }

// ParsedAPDU holds the result of parsing any ACSE APDU.
type ParsedAPDU struct {
	Type     ApduType
	AARE     *ParsedAARE // populated only when Type == ApduAARE
	AARQ     *AARQInfo   // populated only when Type == ApduAARQ
	Auth     AuthInfo    // populated for AARQ: authentication mechanism + value
	UserData []byte      // for AARQ/AARE: MMS payload; for RLRQ/RLRE/ABRT: user-information if present

	// Reason is the RLRQ/RLRE reason [0], -1 when absent.
	Reason int
	// AbortSource and AbortDiagnostic are the ABRT abort-source [0] and
	// abort-diagnostic [1] (-1 when absent).
	AbortSource     int
	AbortDiagnostic int
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
		// BIT STRING {authentication(0)}: one used bit, seven unused
		// (canonical form, as libiec61850 sends it; MMS-STK-21).
		content = berutil.AppendTLV(content, 0x8a, []byte{0x07, 0x80})

		// [11] mechanism-name: password OID 2.2.3.1
		content = berutil.AppendTLV(content, 0x8b, authMechPasswordOID)

		// [12] authentication-value: graphicString [0]
		authVal := berutil.EncodeTLV(0x80, p.Password)
		content = berutil.AppendTLV(content, 0xac, authVal)
	}

	// [30] user-information (EXTERNAL containing MMS payload)
	if len(mmsPayload) > 0 {
		content = append(content, encodeUserInformation(p.MMSContextID, mmsPayload)...)
	}

	return berutil.EncodeTLV(TagAARQ, content), nil
}

// EncodeAARE builds an AARE APDU (Associate Response) with diagnostic
// acse-service-user null, as libiec61850 does for an accepted
// association.
func EncodeAARE(result int, mmsPayload []byte) []byte {
	return EncodeAAREWith(AAREParams{Result: result, DiagSource: DiagSourceUser}, mmsPayload)
}

// AAREParams holds the fields of an AARE.
type AAREParams struct {
	Result     int
	DiagSource int // DiagSourceUser or DiagSourceProvider; 0 means user
	Diagnostic int
	// MMSContextID is the presentation context id named by the
	// user-information indirect-reference; 0 means 3.
	MMSContextID int
	// RespondingAPTitle and RespondingAEQualifier are encoded as [4]
	// and [5] when the title is set.
	RespondingAPTitle     asn1.ObjectIdentifier
	RespondingAEQualifier int
	// ResponderAuth sets responder-acse-requirements [8] to
	// {authentication}, echoing an AARQ that selected the
	// authentication functional unit (ISO 8650-1).
	ResponderAuth bool
}

// EncodeAAREWith builds an AARE APDU. A rejecting AARE carries the
// MMS Initiate-ErrorPDU, when there is one, as user-information
// (ISO 9506-2 24.1: Initiate-ErrorPDU maps to M-ASSOCIATE response with
// result rejected).
func EncodeAAREWith(p AAREParams, mmsPayload []byte) []byte {
	var content []byte

	// [1] application-context-name
	content = berutil.AppendTLV(content, 0xa1, appContextMmsEncoded)

	// [2] result
	resultEnc := berutil.EncodeTLV(0x02, berutil.EncodeInt(p.Result))
	content = berutil.AppendTLV(content, 0xa2, resultEnc)

	// [3] result-source-diagnostic
	src := byte(0xa1)
	if p.DiagSource == DiagSourceProvider {
		src = 0xa2
	}
	diag := berutil.EncodeTLV(src, berutil.EncodeTLV(0x02, berutil.EncodeInt(p.Diagnostic)))
	content = berutil.AppendTLV(content, 0xa3, diag)

	// [4] responding-AP-title + [5] responding-AE-qualifier
	if len(p.RespondingAPTitle) > 0 {
		if oid, err := marshalOID(p.RespondingAPTitle); err == nil {
			content = berutil.AppendTLV(content, 0xa4, oid)
			content = berutil.AppendTLV(content, 0xa5, encodeAEQualifier(p.RespondingAEQualifier))
		}
	}

	// [8] responder-acse-requirements
	if p.ResponderAuth {
		content = berutil.AppendTLV(content, 0x88, []byte{0x07, 0x80})
	}

	// [30] user-information
	if len(mmsPayload) > 0 {
		content = append(content, encodeUserInformation(p.MMSContextID, mmsPayload)...)
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
	return berutil.EncodeTLV(TagABRT, berutil.EncodeTLV(0x80, berutil.EncodeInt(source)))
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

	result := &ParsedAPDU{Type: ApduType(tag), Reason: -1, AbortSource: -1, AbortDiagnostic: -1}

	switch tag {
	case TagAARQ:
		info := &AARQInfo{ProtocolVersionOK: true}
		result.UserData, result.Auth, err = parseAARQInto(content, info)
		if err != nil {
			return nil, err
		}
		result.AARQ = info
	case TagAARE:
		aare, err := parseAARE(content)
		if err != nil {
			return nil, err
		}
		result.AARE = aare
		result.UserData = aare.UserData
	case TagRLRQ, TagRLRE:
		if err := parseRelease(content, result); err != nil {
			return nil, err
		}
	case TagABRT:
		if err := parseABRT(content, result); err != nil {
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
		case 0xa1: // application-context-name
			oid, err := explicitOID(inner)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE application-context-name: %w", err)
			}
			aare.AppContext = oid
		case 0xa3: // result-source-diagnostic
			dtag, dinner, err := berutil.DecodeTLV(inner)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE result-source-diagnostic: %w", err)
			}
			_, v, err := berutil.DecodeTLV(dinner)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE diagnostic: %w", err)
			}
			val, err := berutil.DecodeInteger(v)
			if err != nil {
				return nil, fmt.Errorf("acse: AARE diagnostic value: %w", err)
			}
			switch dtag {
			case 0xa1:
				aare.DiagSource = DiagSourceUser
			case 0xa2:
				aare.DiagSource = DiagSourceProvider
			default:
				return nil, fmt.Errorf("acse: AARE diagnostic source tag 0x%02x", dtag)
			}
			aare.Diagnostic = val
		case 0x88: // responder-acse-requirements
			aare.ResponderAuth = bitZero(inner)
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
	return parseAARQInto(content, &AARQInfo{})
}

func parseAARQInto(content []byte, info *AARQInfo) (userData []byte, auth AuthInfo, err error) {
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
		case 0x80: // [0] protocol-version BIT STRING {version1(0)}
			info.ProtocolVersionOK = bitZero(inner)
		case 0xa1: // [1] application-context-name
			oid, oidErr := explicitOID(inner)
			if oidErr != nil {
				return nil, AuthInfo{}, fmt.Errorf("acse: AARQ application-context-name: %w", oidErr)
			}
			info.AppContext = oid
		case 0xa2: // [2] called-AP-title
			var oid asn1.ObjectIdentifier
			if _, oidErr := asn1.Unmarshal(inner, &oid); oidErr == nil {
				info.CalledAPTitle = oid
			}
		case 0xa3: // [3] called-AE-qualifier
			var val int
			if _, aeErr := asn1.Unmarshal(inner, &val); aeErr == nil {
				info.CalledAEQualifier = &val
			}
		case 0x8a: // [10] sender-acse-requirements
			info.AuthRequested = bitZero(inner)
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

		switch tag {
		case 0xa0, 0x81: // single-ASN1-type, octet-aligned (X.208 EXTERNAL)
			payload = inner
		case 0x82:
			return nil, fmt.Errorf("acse: EXTERNAL arbitrary (BIT STRING) encoding not supported")
		}
	}

	if payload == nil {
		return nil, fmt.Errorf("acse: EXTERNAL missing single-ASN1-type (0xa0) or octet-aligned (0x81) encoding")
	}
	return payload, nil
}

// parseRelease decodes an RLRQ or RLRE: reason [0] IMPLICIT INTEGER
// OPTIONAL, user-information [30] OPTIONAL. Other (extension) elements
// are skipped (ISO 8650-1, MMS-STK-16).
func parseRelease(content []byte, result *ParsedAPDU) error {
	offset := 0
	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return fmt.Errorf("acse: %s field: %w", result.Type, err)
		}
		offset += n
		switch tag {
		case 0x80:
			v, err := berutil.DecodeInteger(inner)
			if err != nil {
				return fmt.Errorf("acse: %s reason: %w", result.Type, err)
			}
			result.Reason = v
		case 0xbe:
			if len(inner) == 0 {
				continue // empty Association-information
			}
			ud, err := parseExternalPayload(inner)
			if err != nil {
				return fmt.Errorf("acse: %s user-information: %w", result.Type, err)
			}
			result.UserData = ud
		}
	}
	return nil
}

// parseABRT decodes an ABRT: abort-source [0] (mandatory),
// abort-diagnostic [1] OPTIONAL, user-information [30] OPTIONAL; other
// elements are skipped (ISO 8650-1, MMS-STK-16).
func parseABRT(content []byte, result *ParsedAPDU) error {
	offset := 0
	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return fmt.Errorf("acse: ABRT field: %w", err)
		}
		offset += n
		switch tag {
		case 0x80, 0x81:
			v, err := berutil.DecodeInteger(inner)
			if err != nil {
				return fmt.Errorf("acse: ABRT field 0x%02x: %w", tag, err)
			}
			if tag == 0x80 {
				result.AbortSource = v
			} else {
				result.AbortDiagnostic = v
			}
		case 0xbe:
			ud, err := parseExternalPayload(inner)
			if err != nil {
				return fmt.Errorf("acse: ABRT user-information: %w", err)
			}
			result.UserData = ud
		}
	}
	if result.AbortSource < 0 {
		return fmt.Errorf("acse: ABRT missing abort-source")
	}
	return nil
}

// bitZero reports whether bit 0 of a BER BIT STRING content is set.
func bitZero(b []byte) bool {
	return len(b) >= 2 && b[1]&0x80 != 0
}

// explicitOID decodes an EXPLICIT-tagged OBJECT IDENTIFIER and returns
// its content octets.
func explicitOID(inner []byte) ([]byte, error) {
	tag, oid, err := berutil.DecodeTLVExact(inner)
	if err != nil {
		return nil, err
	}
	if tag != 0x06 {
		return nil, fmt.Errorf("expected OBJECT IDENTIFIER, got tag 0x%02x", tag)
	}
	return oid, nil
}

func encodeUserInformation(ctxID int, payload []byte) []byte {
	if ctxID == 0 {
		ctxID = 3
	}
	indirectRefTLV := berutil.EncodeTLV(0x02, berutil.EncodeInt(ctxID))
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
