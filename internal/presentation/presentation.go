// Package presentation implements ISO 8823-1 presentation layer PDU
// construction and parsing for the MMS protocol stack.
//
// Only the subset needed by MMS is implemented: CP (Connect Presentation),
// CPA (Connect Presentation Accept), and user-data transfer PPDUs.
// This is internal protocol plumbing — users interact with mms.Client.
package presentation

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/berutil"
)

// Well-known OIDs (pre-encoded BER bytes, excluding the 0x06 tag+length).
var (
	// ACSE abstract syntax: 2.2.1.0.1.
	oidACSE = []byte{0x52, 0x01, 0x00, 0x01}
	// MMS abstract syntax: 1.0.9506.2.1.
	oidMMS = []byte{0x28, 0xca, 0x22, 0x02, 0x01}
	// BER transfer syntax: 2.1.1.
	oidBER = []byte{0x51, 0x01}
)

// Fixed presentation context identifiers.
const (
	ContextIDACSE = 1
	ContextIDMMS  = 3
)

// PpduKind classifies a parsed presentation PDU.
type PpduKind int

const (
	PpduCP       PpduKind = iota // Connect Presentation request
	PpduCPA                      // Connect Presentation Accept
	PpduUserData                 // Data transfer
)

func (k PpduKind) String() string {
	switch k {
	case PpduCP:
		return "CP"
	case PpduCPA:
		return "CPA"
	case PpduUserData:
		return "UserData"
	default:
		return fmt.Sprintf("PpduKind(%d)", int(k))
	}
}

// ConnectParams holds presentation-layer connection parameters.
type ConnectParams struct {
	CallingSelector []byte // calling presentation selector (max 16 bytes)
	CalledSelector  []byte // called presentation selector (max 16 bytes)
}

// ParsedPPDU is the result of parsing a presentation PDU.
type ParsedPPDU struct {
	Kind PpduKind
	// ContextID from the user data's pdv-list
	ContextID int
	// UserData is the inner payload (ACSE or MMS PDU bytes)
	UserData []byte
}

// EncodeCP builds a CP-type PPDU (Connect Presentation, tag 0x31)
// containing the presentation context list (ACSE + MMS) and the
// given ACSE payload as fully-encoded user data.
func EncodeCP(p ConnectParams, acsePayload []byte) []byte {
	ctxList := encodeContextDefinitionList()
	fullyEncoded := encodeFullyEncodedData(ContextIDACSE, acsePayload)

	var normalMode []byte
	if len(p.CallingSelector) > 0 {
		normalMode = berutil.AppendTLV(normalMode, 0x81, p.CallingSelector) // [1] calling-presentation-selector
	}
	if len(p.CalledSelector) > 0 {
		normalMode = berutil.AppendTLV(normalMode, 0x82, p.CalledSelector) // [2] called-presentation-selector
	}
	normalMode = append(normalMode, ctxList...)
	normalMode = append(normalMode, fullyEncoded...)

	modeSelector := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})

	inner := make([]byte, 0, len(modeSelector)+berutil.TLVSize(len(normalMode)))
	inner = append(inner, modeSelector...)
	inner = berutil.AppendTLV(inner, 0xa2, normalMode) // [2] normal-mode-parameters

	return berutil.EncodeTLV(0x31, inner)
}

// EncodeCPA builds a CPA-type PPDU (Connect Presentation Accept, tag 0x31)
// with context results and the given ACSE payload.
func EncodeCPA(respondingSelector []byte, acsePayload []byte) []byte {
	resultList := encodeContextResultList()
	fullyEncoded := encodeFullyEncodedData(ContextIDACSE, acsePayload)

	var normalMode []byte
	if len(respondingSelector) > 0 {
		normalMode = berutil.AppendTLV(normalMode, 0x83, respondingSelector) // [3] responding-presentation-selector
	}
	normalMode = append(normalMode, resultList...)
	normalMode = append(normalMode, fullyEncoded...)

	modeSelector := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})

	inner := make([]byte, 0, len(modeSelector)+berutil.TLVSize(len(normalMode)))
	inner = append(inner, modeSelector...)
	inner = berutil.AppendTLV(inner, 0xa2, normalMode)

	return berutil.EncodeTLV(0x31, inner)
}

// EncodeUserData wraps a payload in a presentation user-data PDU
// (tag 0x61) with the given context ID (ContextIDMMS or ContextIDACSE).
func EncodeUserData(contextID int, payload []byte) []byte {
	return encodeFullyEncodedData(contextID, payload)
}

// Parse parses a presentation PDU, returning the PPDU kind, context ID,
// and inner payload (ACSE or MMS bytes). Handles both CP/CPA (0x31)
// and user-data (0x61) PPDUs.
func Parse(data []byte) (*ParsedPPDU, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("presentation: PPDU too short (%d bytes)", len(data))
	}

	tag := data[0]
	switch tag {
	case 0x31:
		return parseCPorCPA(data)
	case 0x61:
		return parseUserData(data)
	default:
		return nil, fmt.Errorf("presentation: unknown PPDU tag 0x%02x", tag)
	}
}

func parseCPorCPA(data []byte) (*ParsedPPDU, error) {
	_, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("presentation: CP/CPA outer: %w", err)
	}

	result := &ParsedPPDU{Kind: PpduCP}
	hasContextDefList := false
	hasContextResultList := false

	offset := 0
	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("presentation: CP/CPA field: %w", err)
		}
		offset += n

		switch tag {
		case 0xa0: // mode-selector — skip
			continue
		case 0xa2: // normal-mode-parameters
			if err := parseNormalMode(inner, result, &hasContextDefList, &hasContextResultList); err != nil {
				return nil, err
			}
		default:
			// Interop: skip unknown ISO 8823 presentation layer fields.
			// Rejecting them would break interop with different implementations.
		}
	}

	// Distinguish CP vs CPA: CP has context-definition-list (0xa4),
	// CPA has context-definition-result-list (0xa5).
	if hasContextResultList {
		result.Kind = PpduCPA
	}

	return result, nil
}

func parseNormalMode(data []byte, result *ParsedPPDU, hasDefList, hasResultList *bool) error {
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return fmt.Errorf("presentation: normal-mode field: %w", err)
		}
		offset += n

		switch tag {
		case 0x61: // user-data (fully-encoded-data)
			return parsePdvList(inner, result)
		case 0x81, 0x82, 0x83: // presentation selectors — skip
			continue
		case 0xa4: // context-definition-list (CP)
			*hasDefList = true
		case 0xa5: // context-definition-result-list (CPA)
			*hasResultList = true
		default:
			// Interop: skip unknown ISO 8823 presentation layer fields.
			// Rejecting them would break interop with different implementations.
		}
	}
	return nil
}

func parseUserData(data []byte) (*ParsedPPDU, error) {
	_, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("presentation: user-data outer: %w", err)
	}
	result := &ParsedPPDU{Kind: PpduUserData}
	if err := parsePdvList(content, result); err != nil {
		return nil, err
	}
	return result, nil
}

func parsePdvList(data []byte, result *ParsedPPDU) error {
	_, seqContent, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return fmt.Errorf("presentation: pdv-list sequence: %w", err)
	}

	gotContextID := false
	offset := 0
	for offset < len(seqContent) {
		tag, inner, n, err := berutil.DecodeTLVAt(seqContent, offset)
		if err != nil {
			return fmt.Errorf("presentation: pdv-list field: %w", err)
		}
		offset += n

		switch tag {
		case 0x02: // presentation-context-identifier (INTEGER)
			ctxID, err := berutil.DecodeInteger(inner)
			if err != nil {
				return fmt.Errorf("presentation: context-id: %w", err)
			}
			result.ContextID = ctxID
			gotContextID = true
		case 0x06: // transfer-syntax-name — skip
			continue
		case 0xa0: // presentation-data (single-ASN1-type)
			result.UserData = inner
		}
	}
	if offset != len(seqContent) {
		return fmt.Errorf("presentation: pdv-list %d trailing bytes", len(seqContent)-offset)
	}
	if !gotContextID {
		return fmt.Errorf("presentation: pdv-list missing context-id")
	}
	return nil
}

func encodeContextDefinitionList() []byte {
	acseCtx := encodeContextDef(ContextIDACSE, oidACSE, oidBER)
	mmsCtx := encodeContextDef(ContextIDMMS, oidMMS, oidBER)

	content := make([]byte, 0, len(acseCtx)+len(mmsCtx))
	content = append(content, acseCtx...)
	content = append(content, mmsCtx...)

	return berutil.EncodeTLV(0xa4, content)
}

func encodeContextDef(id int, abstractSyntax, transferSyntax []byte) []byte {
	ctxID := berutil.EncodeTLV(0x02, []byte{byte(id)})
	absName := berutil.EncodeTLV(0x06, abstractSyntax)
	tsOID := berutil.EncodeTLV(0x06, transferSyntax)
	tsList := berutil.EncodeTLV(0x30, tsOID)

	content := make([]byte, 0, len(ctxID)+len(absName)+len(tsList))
	content = append(content, ctxID...)
	content = append(content, absName...)
	content = append(content, tsList...)

	return berutil.EncodeTLV(0x30, content)
}

func encodeContextResultList() []byte {
	acseResult := encodeAcceptResult()
	mmsResult := encodeAcceptResult()

	content := make([]byte, 0, len(acseResult)+len(mmsResult))
	content = append(content, acseResult...)
	content = append(content, mmsResult...)

	return berutil.EncodeTLV(0xa5, content)
}

func encodeAcceptResult() []byte {
	return berutil.EncodeTLV(0x30, []byte{
		0x80, 0x01, 0x00,
		0x81, byte(len(oidBER)), oidBER[0], oidBER[1],
	})
}

func encodeFullyEncodedData(contextID int, payload []byte) []byte {
	ctxIDEnc := berutil.EncodeTLV(0x02, []byte{byte(contextID)})
	presData := berutil.EncodeTLV(0xa0, payload)

	pdvContent := make([]byte, 0, len(ctxIDEnc)+len(presData))
	pdvContent = append(pdvContent, ctxIDEnc...)
	pdvContent = append(pdvContent, presData...)

	pdvList := berutil.EncodeTLV(0x30, pdvContent)
	return berutil.EncodeTLV(0x61, pdvList)
}
