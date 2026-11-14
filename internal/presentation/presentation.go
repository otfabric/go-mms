// SPDX-License-Identifier: MIT

// Package presentation implements ISO 8823-1 presentation layer PDU
// construction and parsing for the MMS protocol stack.
//
// Only the subset needed by MMS (normal mode, kernel functional unit) is
// implemented: CP, CPA and CPR (connect, accept, reject), ARU and ARP
// (user and provider abort), and fully-encoded user data. Presentation
// contexts are negotiated per ISO 8823-1: the responder answers every
// proposed context, in proposal order, and the identifiers the
// initiator chose are used for the data phase.
// This is internal protocol plumbing — users interact with mms.Client.
package presentation

import (
	"bytes"
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

// Presentation context identifiers this implementation proposes as an
// initiator. They are odd, as ISO 8823-1 requires of initiator-assigned
// identifiers; a responder uses the identifiers the peer proposed.
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
	PpduCPR                      // Connect Presentation Reject
	PpduARU                      // Abnormal Release by the User
	PpduARP                      // Abnormal Release by the Provider
)

func (k PpduKind) String() string {
	switch k {
	case PpduCP:
		return "CP"
	case PpduCPA:
		return "CPA"
	case PpduUserData:
		return "UserData"
	case PpduCPR:
		return "CPR"
	case PpduARU:
		return "ARU"
	case PpduARP:
		return "ARP"
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
	// ContextID from the user data's first PDV-list
	ContextID int
	// UserData is the inner payload (ACSE or MMS PDU bytes) of the
	// first PDV-list
	UserData []byte
	// PDVs lists every PDV-list of fully-encoded user data, in order.
	PDVs []PDV

	// Contexts is the presentation-context-definition-list of a CP.
	Contexts []ContextDef
	// Results is the presentation-context-definition-result-list of a
	// CPA or CPR; HasResults reports whether it was present.
	Results    []ContextResult
	HasResults bool
	// ProviderReason is the provider-reason of a CPR ([10]) or ARP
	// ([0]); -1 when absent.
	ProviderReason int
}

// PDV is one PDV-list of fully-encoded data.
type PDV struct {
	ContextID int
	Data      []byte
}

// ContextDef is one proposed presentation context: the identifier the
// initiator chose, the abstract syntax name and the proposed transfer
// syntax names (raw OID contents, without tag and length).
type ContextDef struct {
	ID               int
	AbstractSyntax   []byte
	TransferSyntaxes [][]byte
}

// IsACSE reports whether the context carries the ACSE abstract syntax
// (2.2.1.0.1).
func (c ContextDef) IsACSE() bool { return bytes.Equal(c.AbstractSyntax, oidACSE) }

// IsMMS reports whether the context carries the MMS abstract syntax
// (1.0.9506.2.1, ISO 9506-2 24.11).
func (c ContextDef) IsMMS() bool { return bytes.Equal(c.AbstractSyntax, oidMMS) }

// OffersBER reports whether BER (2.1.1, ISO 9506-2 Annex A.6) is among
// the proposed transfer syntaxes.
func (c ContextDef) OffersBER() bool {
	for _, ts := range c.TransferSyntaxes {
		if bytes.Equal(ts, oidBER) {
			return true
		}
	}
	return false
}

// Result values of a Result-list entry (ISO 8823-1 Result).
const (
	ResultAcceptance        = 0
	ResultUserRejection     = 1
	ResultProviderRejection = 2
)

// Provider reasons of a rejected context in a Result-list (ISO 8823-1
// Result-list provider-reason [2]).
const (
	ContextReasonNotSpecified                 = 0
	ContextReasonAbstractSyntaxNotSupported   = 1
	ContextReasonTransferSyntaxesNotSupported = 2
	ContextReasonLocalLimitOnDCSExceeded      = 3
)

// Provider-reason values of a CPR-PPDU ([10]) and an ARP-PPDU (ISO
// 8823-1 Provider-reason / Abort-reason).
const (
	ProviderReasonNotSpecified        = 0
	ProviderReasonTemporaryCongestion = 1
	ProviderReasonLocalLimitExceeded  = 2
)

// ContextResult is one entry of a presentation-context-definition-result
// list. TransferSyntax is set for an accepted context; ProviderReason is
// -1 unless the context was rejected by the provider.
type ContextResult struct {
	Result         int
	TransferSyntax []byte
	ProviderReason int
}

// Negotiation is a responder's answer to a CP's context definition list.
type Negotiation struct {
	// Results has one entry per proposed context, in proposal order.
	Results []ContextResult
	// ACSE and MMS are the identifiers of the accepted ACSE and MMS
	// contexts, 0 when none was accepted.
	ACSE, MMS int
}

// Negotiate answers a proposed context definition list (ISO 8823-1;
// ISO 9506-2 Annex A.5): the ACSE and MMS abstract syntaxes are accepted
// with BER (ISO 9506-2 Annex A.6) when BER is offered, every other
// context is rejected by the provider with abstract-syntax-not-supported
// or proposed-transfer-syntaxes-not-supported. Only the first context
// of each abstract syntax is accepted.
func Negotiate(defs []ContextDef) Negotiation {
	var n Negotiation
	for _, d := range defs {
		known := d.IsACSE() || d.IsMMS()
		switch {
		case !known:
			n.Results = append(n.Results, ContextResult{Result: ResultProviderRejection, ProviderReason: ContextReasonAbstractSyntaxNotSupported})
		case !d.OffersBER():
			n.Results = append(n.Results, ContextResult{Result: ResultProviderRejection, ProviderReason: ContextReasonTransferSyntaxesNotSupported})
		case d.IsACSE() && n.ACSE == 0:
			n.ACSE = d.ID
			n.Results = append(n.Results, ContextResult{Result: ResultAcceptance, TransferSyntax: oidBER, ProviderReason: -1})
		case d.IsMMS() && n.MMS == 0:
			n.MMS = d.ID
			n.Results = append(n.Results, ContextResult{Result: ResultAcceptance, TransferSyntax: oidBER, ProviderReason: -1})
		default:
			// A second context for a syntax already accepted.
			n.Results = append(n.Results, ContextResult{Result: ResultProviderRejection, ProviderReason: ContextReasonLocalLimitOnDCSExceeded})
		}
	}
	return n
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

// EncodeCPA builds a CPA-type PPDU (Connect Presentation Accept, tag
// 0x31) accepting the two contexts this implementation proposes (ACSE 1,
// MMS 3), with the given ACSE payload.
func EncodeCPA(respondingSelector []byte, acsePayload []byte) []byte {
	accept := ContextResult{Result: ResultAcceptance, TransferSyntax: oidBER, ProviderReason: -1}
	return EncodeCPAResults(respondingSelector, []ContextResult{accept, accept}, ContextIDACSE, acsePayload)
}

// EncodeCPAResults builds a CPA-type PPDU with the given result list
// (one entry per proposed context, in proposal order) and the ACSE
// payload as fully-encoded data on context acseContextID.
func EncodeCPAResults(respondingSelector []byte, results []ContextResult, acseContextID int, acsePayload []byte) []byte {
	var normalMode []byte
	if len(respondingSelector) > 0 {
		normalMode = berutil.AppendTLV(normalMode, 0x83, respondingSelector) // [3] responding-presentation-selector
	}
	normalMode = append(normalMode, encodeContextResultList(results)...)
	normalMode = append(normalMode, encodeFullyEncodedData(acseContextID, acsePayload)...)

	modeSelector := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})

	inner := make([]byte, 0, len(modeSelector)+berutil.TLVSize(len(normalMode)))
	inner = append(inner, modeSelector...)
	inner = berutil.AppendTLV(inner, 0xa2, normalMode)

	return berutil.EncodeTLV(0x31, inner)
}

// EncodeCPR builds a CPR-PPDU (Connect Presentation Reject) in normal
// mode (ISO 8823-1: normal-mode-parameters SEQUENCE { protocol-version
// [0] DEFAULT, responding-presentation-selector [3], presentation-
// context-definition-result-list [5], default-context-result [7],
// provider-reason [10], user-data }). A rejection by the presentation
// user carries the rejecting ACSE AARE as user data and no
// provider-reason (providerReason < 0); a rejection by the provider
// carries a provider-reason and normally no user data (acsePayload nil).
func EncodeCPR(respondingSelector []byte, results []ContextResult, providerReason, acseContextID int, acsePayload []byte) []byte {
	var content []byte
	if len(respondingSelector) > 0 {
		content = berutil.AppendTLV(content, 0x83, respondingSelector)
	}
	if results != nil {
		content = append(content, encodeContextResultList(results)...)
	}
	if providerReason >= 0 {
		content = berutil.AppendTLV(content, 0x8a, berutil.EncodeInt(providerReason))
	}
	if acsePayload != nil {
		content = append(content, encodeFullyEncodedData(acseContextID, acsePayload)...)
	}
	return berutil.EncodeTLV(0x30, content)
}

// EncodeARU builds an ARU-PPDU (abnormal release by the user) in normal
// mode: [0] IMPLICIT SEQUENCE { user-data } carrying the ACSE ABRT on
// context acseContextID (ISO 8823-1; ISO 9506-2 Annex A.3 maps
// M-U-ABORT to A-U-ABORT). The optional presentation-context-identifier-
// list is omitted, as libiec61850 does.
func EncodeARU(acseContextID int, acsePayload []byte) []byte {
	return berutil.EncodeTLV(0xa0, encodeFullyEncodedData(acseContextID, acsePayload))
}

// ParseAbort parses the presentation PDU carried by a session ABORT: an
// ARU-PPDU (0xa0, normal mode) or an ARP-PPDU (0x30 SEQUENCE {
// provider-reason [0] OPTIONAL, event-identifier [1] OPTIONAL }).
func ParseAbort(data []byte) (*ParsedPPDU, error) {
	tag, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("presentation: abort PPDU: %w", err)
	}
	switch tag {
	case 0xa0:
		result := &ParsedPPDU{Kind: PpduARU, ProviderReason: -1}
		err := walkTLVs(content, func(t byte, inner []byte) error {
			switch t {
			case 0x61:
				return parseFullyEncoded(inner, result)
			case 0xa0: // presentation-context-identifier-list
			default:
				// Interop: skip unknown ARU fields.
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	case 0x30:
		result := &ParsedPPDU{Kind: PpduARP, ProviderReason: -1}
		err := walkTLVs(content, func(t byte, inner []byte) error {
			if t == 0x80 {
				v, err := berutil.DecodeInteger(inner)
				if err != nil {
					return fmt.Errorf("presentation: ARP provider-reason: %w", err)
				}
				result.ProviderReason = v
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	default:
		return nil, fmt.Errorf("presentation: unknown abort PPDU tag 0x%02x", tag)
	}
}

func walkTLVs(data []byte, fn func(tag byte, inner []byte) error) error {
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return fmt.Errorf("presentation: field: %w", err)
		}
		offset += n
		if err := fn(tag, inner); err != nil {
			return err
		}
	}
	return nil
}

// EncodeUserData wraps a payload in a presentation user-data PDU
// (tag 0x61) with the given context ID (ContextIDMMS or ContextIDACSE).
func EncodeUserData(contextID int, payload []byte) []byte {
	return encodeFullyEncodedData(contextID, payload)
}

// Parse parses a presentation PDU, returning the PPDU kind, context ID,
// and inner payload (ACSE or MMS bytes). Handles both CP/CPA (0x31)
// user-data (0x61) and CPR (0x30) PPDUs. Abort PPDUs are parsed with
// ParseAbort, since an ARP and a CPR share the SEQUENCE tag.
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
	case 0x30:
		return parseCPR(data)
	default:
		return nil, fmt.Errorf("presentation: unknown PPDU tag 0x%02x", tag)
	}
}

func parseCPorCPA(data []byte) (*ParsedPPDU, error) {
	_, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("presentation: CP/CPA outer: %w", err)
	}

	result := &ParsedPPDU{Kind: PpduCP, ProviderReason: -1}
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
	return walkTLVs(data, func(tag byte, inner []byte) error {
		switch tag {
		case 0x61: // user-data (fully-encoded-data)
			return parseFullyEncoded(inner, result)
		case 0x81, 0x82, 0x83: // presentation selectors — skip
		case 0xa4: // context-definition-list (CP)
			*hasDefList = true
			defs, err := parseContextDefList(inner)
			if err != nil {
				return err
			}
			result.Contexts = defs
		case 0xa5: // context-definition-result-list (CPA)
			*hasResultList = true
			res, err := parseResultList(inner)
			if err != nil {
				return err
			}
			result.Results, result.HasResults = res, true
		default:
			// Interop: skip unknown ISO 8823 presentation layer fields.
			// Rejecting them would break interop with different implementations.
		}
		return nil
	})
}

// parseCPR parses a normal-mode CPR-PPDU (0x30 SEQUENCE).
func parseCPR(data []byte) (*ParsedPPDU, error) {
	_, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("presentation: CPR outer: %w", err)
	}
	result := &ParsedPPDU{Kind: PpduCPR, ProviderReason: -1}
	err = walkTLVs(content, func(tag byte, inner []byte) error {
		switch tag {
		case 0xa5:
			res, err := parseResultList(inner)
			if err != nil {
				return err
			}
			result.Results, result.HasResults = res, true
		case 0x8a: // provider-reason [10]
			v, err := berutil.DecodeInteger(inner)
			if err != nil {
				return fmt.Errorf("presentation: CPR provider-reason: %w", err)
			}
			result.ProviderReason = v
		case 0x61:
			return parseFullyEncoded(inner, result)
		default:
			// protocol-version [0], responding selector [3],
			// default-context-result [7]: not needed.
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// maxProposedContexts bounds the presentation-context-definition-list
// of a CP (ISO 8823-1). An MMS association uses two contexts, ACSE and
// MMS, and the stacks in INTEROP.md propose two; a longer list is
// refused, so that the answer (one result per proposed context) always
// fits a Session REFUSE or ACCEPT.
const maxProposedContexts = 16

// parseContextDefList decodes a presentation-context-definition-list:
// SEQUENCE OF SEQUENCE { presentation-context-identifier INTEGER,
// abstract-syntax-name OID, transfer-syntax-name-list SEQUENCE OF OID }.
// A list of more than maxProposedContexts definitions is an error.
func parseContextDefList(data []byte) ([]ContextDef, error) {
	var defs []ContextDef
	err := walkTLVs(data, func(tag byte, inner []byte) error {
		if tag != 0x30 {
			return fmt.Errorf("presentation: context definition tag 0x%02x", tag)
		}
		if len(defs) == maxProposedContexts {
			return fmt.Errorf("presentation: more than %d proposed presentation contexts", maxProposedContexts)
		}
		d := ContextDef{ID: -1}
		err := walkTLVs(inner, func(t byte, v []byte) error {
			switch t {
			case 0x02:
				id, err := berutil.DecodeInteger(v)
				if err != nil {
					return fmt.Errorf("presentation: context identifier: %w", err)
				}
				d.ID = id
			case 0x06:
				d.AbstractSyntax = v
			case 0x30:
				return walkTLVs(v, func(tt byte, ts []byte) error {
					if tt == 0x06 {
						d.TransferSyntaxes = append(d.TransferSyntaxes, ts)
					}
					return nil
				})
			}
			return nil
		})
		if err != nil {
			return err
		}
		if d.ID < 0 || d.AbstractSyntax == nil {
			return fmt.Errorf("presentation: context definition without identifier or abstract syntax")
		}
		defs = append(defs, d)
		return nil
	})
	return defs, err
}

// parseResultList decodes a presentation-context-definition-result-list:
// SEQUENCE OF SEQUENCE { result [0] Result, transfer-syntax-name [1]
// OPTIONAL, provider-reason [2] OPTIONAL }.
func parseResultList(data []byte) ([]ContextResult, error) {
	res := []ContextResult{}
	err := walkTLVs(data, func(tag byte, inner []byte) error {
		if tag != 0x30 {
			return fmt.Errorf("presentation: result list entry tag 0x%02x", tag)
		}
		r := ContextResult{Result: -1, ProviderReason: -1}
		err := walkTLVs(inner, func(t byte, v []byte) error {
			var err error
			switch t {
			case 0x80:
				r.Result, err = berutil.DecodeInteger(v)
			case 0x81:
				r.TransferSyntax = v
			case 0x82:
				r.ProviderReason, err = berutil.DecodeInteger(v)
			}
			if err != nil {
				return fmt.Errorf("presentation: result list entry: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if r.Result < 0 {
			return fmt.Errorf("presentation: result list entry without result")
		}
		res = append(res, r)
		return nil
	})
	return res, err
}

func parseUserData(data []byte) (*ParsedPPDU, error) {
	_, content, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return nil, fmt.Errorf("presentation: user-data outer: %w", err)
	}
	result := &ParsedPPDU{Kind: PpduUserData, ProviderReason: -1}
	if err := parseFullyEncoded(content, result); err != nil {
		return nil, err
	}
	return result, nil
}

// parseFullyEncoded parses the PDV-lists of Fully-encoded-data
// (SEQUENCE SIZE(1..MAX) OF PDV-list). The first PDV-list fills
// ContextID and UserData; all are listed in PDVs.
func parseFullyEncoded(data []byte, result *ParsedPPDU) error {
	if len(data) == 0 {
		return fmt.Errorf("presentation: fully-encoded-data without PDV-list")
	}
	offset := 0
	for offset < len(data) {
		_, _, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return fmt.Errorf("presentation: pdv-list sequence: %w", err)
		}
		pdv := &ParsedPPDU{}
		if err := parsePdvList(data[offset:offset+n], pdv); err != nil {
			return err
		}
		if offset == 0 {
			result.ContextID, result.UserData = pdv.ContextID, pdv.UserData
		}
		result.PDVs = append(result.PDVs, PDV{ContextID: pdv.ContextID, Data: pdv.UserData})
		offset += n
	}
	return nil
}

// parsePdvList parses one PDV-list: SEQUENCE { transfer-syntax-name
// OPTIONAL, presentation-context-identifier, presentation-data-values
// CHOICE { single-ASN1-type [0], octet-aligned [1] IMPLICIT OCTET
// STRING, arbitrary [2] IMPLICIT BIT STRING } } (ISO 8823-1). Both
// single-ASN1-type and octet-aligned carry a BER-encoded APDU here;
// arbitrary cannot hold one and is refused.
func parsePdvList(data []byte, result *ParsedPPDU) error {
	tag, seqContent, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return fmt.Errorf("presentation: pdv-list sequence: %w", err)
	}
	if tag != 0x30 {
		return fmt.Errorf("presentation: pdv-list tag 0x%02x, want 0x30", tag)
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
		case 0xa0, 0x81: // single-ASN1-type, octet-aligned
			result.UserData = inner
		case 0x82:
			return fmt.Errorf("presentation: pdv-list arbitrary (BIT STRING) encoding not supported")
		default:
			return fmt.Errorf("presentation: pdv-list unexpected tag 0x%02x", tag)
		}
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
	ctxID := berutil.EncodeTLV(0x02, berutil.EncodeInt(id))
	absName := berutil.EncodeTLV(0x06, abstractSyntax)
	tsOID := berutil.EncodeTLV(0x06, transferSyntax)
	tsList := berutil.EncodeTLV(0x30, tsOID)

	content := make([]byte, 0, len(ctxID)+len(absName)+len(tsList))
	content = append(content, ctxID...)
	content = append(content, absName...)
	content = append(content, tsList...)

	return berutil.EncodeTLV(0x30, content)
}

func encodeContextResultList(results []ContextResult) []byte {
	var content []byte
	for _, r := range results {
		entry := berutil.EncodeTLV(0x80, berutil.EncodeInt(r.Result))
		if r.Result == ResultAcceptance && len(r.TransferSyntax) > 0 {
			entry = berutil.AppendTLV(entry, 0x81, r.TransferSyntax)
		}
		if r.Result == ResultProviderRejection && r.ProviderReason >= 0 {
			entry = berutil.AppendTLV(entry, 0x82, berutil.EncodeInt(r.ProviderReason))
		}
		content = berutil.AppendTLV(content, 0x30, entry)
	}
	return berutil.EncodeTLV(0xa5, content)
}

func encodeFullyEncodedData(contextID int, payload []byte) []byte {
	ctxIDEnc := berutil.EncodeTLV(0x02, berutil.EncodeInt(contextID))
	presData := berutil.EncodeTLV(0xa0, payload)

	pdvContent := make([]byte, 0, len(ctxIDEnc)+len(presData))
	pdvContent = append(pdvContent, ctxIDEnc...)
	pdvContent = append(pdvContent, presData...)

	pdvList := berutil.EncodeTLV(0x30, pdvContent)
	return berutil.EncodeTLV(0x61, pdvList)
}
