// SPDX-License-Identifier: MIT

// Package session implements ISO 8327-1 session layer SPDU construction
// and parsing for the MMS protocol stack.
//
// Only the subset of session services needed by MMS (kernel and duplex
// functional units) is implemented: CONNECT (CN), ACCEPT (AC), REFUSE
// (RF), DATA TRANSFER with GIVE TOKENS (DT/GT), FINISH (FN), DISCONNECT
// (DN), ABORT (AB) and ABORT ACCEPT (AA) SPDUs. Tokens, activities,
// synchronisation and segmenting are not implemented.
// This is internal protocol plumbing — users interact with mms.Client.
package session

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// SPDU type identifiers (SI — Session Identifier, ISO 8327-1 8.3).
const (
	SIData        byte = 0x01 // Give Tokens / Data Transfer
	SINotFinish   byte = 0x08
	SIFinish      byte = 0x09
	SIDisconnect  byte = 0x0a
	SIRefuse      byte = 0x0c
	SIConnect     byte = 0x0d
	SIAccept      byte = 0x0e
	SIAbort       byte = 0x19
	SIAbortAccept byte = 0x1a
)

// Parameter group identifiers (PGI, ISO 8327-1 8.3). Only Connection
// Identifier, Connect/Accept Item and the two user data groups are PGIs.
const (
	pgiConnectionID     byte = 0x01 // 1
	pgiConnAcceptItem   byte = 0x05 // 5
	pgiUserData         byte = 0xc1 // 193
	pgiExtendedUserData byte = 0xc2 // 194
)

// Parameter identifiers (PI, ISO 8327-1 8.3).
const (
	piTransportDisc   byte = 0x11 // 17
	piProtocolOpts    byte = 0x13 // 19
	piSessionUserReq  byte = 0x14 // 20
	piVersion         byte = 0x16 // 22
	piReasonCode      byte = 0x32 // 50
	piCallingSelector byte = 0x33 // 51
	piCalledSelector  byte = 0x34 // 52
)

// Limits and fixed parameter values (ISO 8327-1 8.3).
const (
	maxSelectorLen         = 16    // Calling/Called Session Selector
	maxConnectUserData     = 512   // User Data PGI in a CONNECT
	maxConnectExtUserData  = 10240 // Extended User Data PGI in a CONNECT (version 2)
	transportDiscReleased  = 0x01  // Transport Disconnect: transport connection released
	transportDiscUserAbort = 0x0b  // released | user abort | no reason (as libiec61850)
)

// Protocol version bits of the Version Number PI (ISO 8327-1 8.3.1.7).
const (
	Version1 byte = 0x01
	Version2 byte = 0x02
)

// FUDuplex is the duplex functional unit bit of Session User
// Requirements (ISO 8327-1 8.3.1.11). MMS requires full duplex
// (ISO 9506-2 24.7).
const FUDuplex uint16 = 0x0002

// REFUSE reason codes (Reason Code PI 50, ISO 8327-1 8.3.4.x).
const (
	ReasonUserNotSpecified    byte = 0   // rejection by called SS-user, reason not specified
	ReasonUserCongestion      byte = 1   // rejection by called SS-user due to temporary congestion
	ReasonUserData            byte = 2   // rejection by called SS-user; user data follows
	ReasonSelectorUnknown     byte = 129 // session selector unknown
	ReasonNotAttached         byte = 130 // SS-user not attached to SSAP
	ReasonSPMCongestion       byte = 131 // SPM congestion at connect time
	ReasonVersionNotSupported byte = 132 // proposed protocol versions not supported
	ReasonSPMNotSpecified     byte = 133 // rejection by the SPM, reason not specified
	ReasonSPMRestriction      byte = 134 // rejection by the SPM, implementation restriction
)

// ReasonTransient reports whether a REFUSE reason code describes a
// temporary condition (congestion) after which a new attempt may succeed.
func ReasonTransient(reason byte) bool {
	return reason == ReasonUserCongestion || reason == ReasonSPMCongestion
}

// SpduType classifies a parsed SPDU.
type SpduType byte

const (
	SpduConnect     SpduType = SpduType(SIConnect)
	SpduAccept      SpduType = SpduType(SIAccept)
	SpduData        SpduType = SpduType(SIData)
	SpduFinish      SpduType = SpduType(SIFinish)
	SpduDisconnect  SpduType = SpduType(SIDisconnect)
	SpduAbort       SpduType = SpduType(SIAbort)
	SpduRefuse      SpduType = SpduType(SIRefuse)
	SpduAbortAccept SpduType = SpduType(SIAbortAccept)
)

func (t SpduType) String() string {
	switch t {
	case SpduConnect:
		return "CONNECT"
	case SpduAccept:
		return "ACCEPT"
	case SpduData:
		return "DATA"
	case SpduFinish:
		return "FINISH"
	case SpduDisconnect:
		return "DISCONNECT"
	case SpduAbort:
		return "ABORT"
	case SpduRefuse:
		return "REFUSE"
	case SpduAbortAccept:
		return "ABORT-ACCEPT"
	default:
		return fmt.Sprintf("SpduType(0x%02x)", byte(t))
	}
}

// ConnectParams holds session-layer parameters for CONNECT and ACCEPT SPDUs.
type ConnectParams struct {
	CallingSelector []byte // calling session selector (max 16 octets)
	CalledSelector  []byte // called session selector (max 16 octets)

	// Version is the Version Number PI: in a CONNECT the bit mask of
	// proposed versions, in an ACCEPT the single selected version. Zero
	// means Version2.
	Version byte

	// Requirements is the Session User Requirements PI (functional
	// units). Zero means FUDuplex.
	Requirements uint16
}

// ErrSelectorTooLong is returned when a session selector exceeds 16
// octets (ISO 8327-1 Calling/Called Session Selector).
var ErrSelectorTooLong = errors.New("session: session selector longer than 16 octets")

// ParsedSpdu is the result of parsing a session PDU.
type ParsedSpdu struct {
	Type            SpduType
	CallingSelector []byte
	CalledSelector  []byte
	UserData        []byte // slice into the original input; not copied

	// Version is the Version Number PI of a CONNECT or ACCEPT (0 when
	// absent, which ISO 8327-1 reads as version 1).
	Version byte
	// HasRequirements reports whether Session User Requirements was
	// present; Requirements is its value.
	HasRequirements bool
	Requirements    uint16
	// HasReason reports whether a REFUSE carried the Reason Code PI;
	// Reason is its first octet (the user data, if any, is UserData).
	HasReason bool
	Reason    byte
}

// EncodeConnect builds a CONNECT SPDU (SI=0x0d) with the given parameters
// and user data (the presentation layer payload). User data up to 512
// octets goes in the User Data PGI (193); longer user data, up to 10 240
// octets, in the Extended User Data PGI (194), which requires version 2
// to be proposed (ISO 8327-1 8.3.1.x).
func EncodeConnect(p ConnectParams, userData []byte) ([]byte, error) {
	return encodeConnectAccept(SIConnect, p, userData)
}

// EncodeAccept builds an ACCEPT SPDU (SI=0x0e) with the given parameters
// and user data.
func EncodeAccept(p ConnectParams, userData []byte) ([]byte, error) {
	return encodeConnectAccept(SIAccept, p, userData)
}

func encodeConnectAccept(si byte, p ConnectParams, userData []byte) ([]byte, error) {
	if len(p.CallingSelector) > maxSelectorLen || len(p.CalledSelector) > maxSelectorLen {
		return nil, ErrSelectorTooLong
	}
	version := p.Version
	if version == 0 {
		version = Version2
	}
	reqs := p.Requirements
	if reqs == 0 {
		reqs = FUDuplex
	}

	// Connection/Accept Item: PI 19 (protocol options) + PI 22 (version).
	connAccept := []byte{
		piProtocolOpts, 0x01, 0x00, // protocol options = 0
		piVersion, 0x01, version,
	}

	var params []byte
	// PGI 5 — Connection/Accept Item
	params = appendPGI(params, pgiConnAcceptItem, connAccept)
	// PI 20 — Session User Requirements
	params = append(params, piSessionUserReq, 0x02, byte(reqs>>8), byte(reqs))
	// PI 51 — Calling Session Selector
	if len(p.CallingSelector) > 0 {
		params = appendPGI(params, piCallingSelector, p.CallingSelector)
	}
	// PI 52 — Called Session Selector
	if len(p.CalledSelector) > 0 {
		params = appendPGI(params, piCalledSelector, p.CalledSelector)
	}
	// PGI 193 — User Data, or PGI 194 — Extended User Data in a CONNECT.
	if len(userData) > 0 {
		tag := pgiUserData
		if si == SIConnect && len(userData) > maxConnectUserData {
			if len(userData) > maxConnectExtUserData || version&Version2 == 0 {
				return nil, fmt.Errorf("session: CONNECT user data %d octets exceeds the version limit", len(userData))
			}
			tag = pgiExtendedUserData
		}
		params = appendPGI(params, tag, userData)
	}
	return encodeSPDU(si, params)
}

// encodeSPDU prefixes params with SI and LI. LI uses the one-octet form
// up to 254 and 0xff plus two octets up to 65535 (ISO 8327-1 LI field); a
// longer SPDU cannot be encoded.
func encodeSPDU(si byte, params []byte) ([]byte, error) {
	if len(params) > maxLI {
		return nil, fmt.Errorf("session: SPDU 0x%02x parameters %d bytes exceed %d", si, len(params), maxLI)
	}
	buf := make([]byte, 0, 1+liSize(len(params))+len(params))
	buf = append(buf, si)
	buf = appendLI(buf, len(params))
	return append(buf, params...), nil
}

// dataSpduHeader is the fixed 4-byte DATA SPDU prefix.
var dataSpduHeader = []byte{0x01, 0x00, 0x01, 0x00}

// EncodeData builds a DATA SPDU (Give Tokens + Data Transfer).
// The payload is the presentation layer PDU.
func EncodeData(userData []byte) []byte {
	buf := make([]byte, 0, 4+len(userData))
	buf = append(buf, dataSpduHeader...)
	buf = append(buf, userData...)
	return buf
}

// EncodeFinish builds a FINISH SPDU (SI=0x09) carrying user data.
func EncodeFinish(userData []byte) ([]byte, error) {
	return encodeSimpleWithUserData(SIFinish, userData)
}

// EncodeDisconnect builds a DISCONNECT SPDU (SI=0x0a) carrying user data.
func EncodeDisconnect(userData []byte) ([]byte, error) {
	return encodeSimpleWithUserData(SIDisconnect, userData)
}

func encodeSimpleWithUserData(si byte, userData []byte) ([]byte, error) {
	return encodeSPDU(si, appendPGI(nil, pgiUserData, userData))
}

// EncodeAbort builds an ABORT SPDU (SI=0x19).
func EncodeAbort(userData []byte) ([]byte, error) {
	params := []byte{piTransportDisc, 0x01, transportDiscUserAbort}
	if len(userData) > 0 {
		params = appendPGI(params, pgiUserData, userData)
	}
	return encodeSPDU(SIAbort, params)
}

// EncodeRefuse builds a REFUSE SPDU (SI=0x0c): Transport Disconnect PI 17
// (transport connection released) and Reason Code PI 50. With version 2
// the SS-user data follows the reason octet when reason is
// ReasonUserData; for other reasons userData must be empty
// (ISO 8327-1 8.3.4).
func EncodeRefuse(reason byte, userData []byte) ([]byte, error) {
	if reason != ReasonUserData && len(userData) > 0 {
		return nil, fmt.Errorf("session: REFUSE reason %d cannot carry user data", reason)
	}
	params := []byte{piTransportDisc, 0x01, transportDiscReleased}
	rc := make([]byte, 0, 1+len(userData))
	rc = append(rc, reason)
	rc = append(rc, userData...)
	params = appendPGI(params, piReasonCode, rc)
	return encodeSPDU(SIRefuse, params)
}

// Parse parses a session SPDU from raw data, returning the SPDU type,
// any session selectors, and the user data payload.
func Parse(data []byte) (*ParsedSpdu, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("session: SPDU too short (%d bytes)", len(data))
	}

	si := data[0]
	result := &ParsedSpdu{Type: SpduType(si)}

	switch si {
	case SIData:
		return parseData(data, result)
	case SIConnect, SIAccept:
		return parseConnectAccept(data, result)
	case SIFinish, SIDisconnect, SIAbort, SIAbortAccept:
		return parseSimpleWithUserData(data, result)
	case SIRefuse:
		return parseRefuse(data, result)
	default:
		return nil, fmt.Errorf("session: unknown SPDU type 0x%02x", si)
	}
}

func parseData(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("session: DATA SPDU too short (%d bytes)", len(data))
	}
	if data[0] != 0x01 || data[1] != 0x00 || data[2] != 0x01 || data[3] != 0x00 {
		return nil, fmt.Errorf("session: DATA SPDU invalid header %x, expected 01000100", data[:4])
	}
	result.UserData = data[4:]
	return result, nil
}

// header returns the parameter field of an SPDU (after SI and LI).
func header(data []byte, t SpduType) ([]byte, error) {
	li, liLen, err := decodePGILength(data[1:])
	if err != nil {
		return nil, fmt.Errorf("session: %s SPDU length: %w", t, err)
	}
	start := 1 + liLen
	if len(data) < start+li {
		return nil, fmt.Errorf("session: %s SPDU truncated (need %d, have %d)", t, start+li, len(data))
	}
	return data[start : start+li], nil
}

// walkParams calls fn for each PI/PGI in params. A final fragment shorter
// than a code and a length octet is reported as trailing bytes.
func walkParams(params []byte, t SpduType, fn func(code byte, val []byte) error) error {
	offset := 0
	for offset+2 <= len(params) {
		code := params[offset]
		l, n, err := decodePGILength(params[offset+1:])
		if err != nil {
			return fmt.Errorf("session: %s SPDU parameter length: %w", t, err)
		}
		offset += 1 + n
		end := offset + l
		if end > len(params) {
			return fmt.Errorf("session: %s SPDU parameter 0x%02x truncated", t, code)
		}
		if err := fn(code, params[offset:end]); err != nil {
			return err
		}
		offset = end
	}
	if offset != len(params) {
		return fmt.Errorf("session: %s SPDU %d trailing bytes in header", t, len(params)-offset)
	}
	return nil
}

func parseConnectAccept(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	params, err := header(data, result.Type)
	if err != nil {
		return nil, err
	}
	hasUD, hasExtUD := false, false
	err = walkParams(params, result.Type, func(code byte, val []byte) error {
		switch code {
		case piCallingSelector:
			result.CallingSelector = val
		case piCalledSelector:
			result.CalledSelector = val
		case pgiUserData:
			hasUD = true
			result.UserData = val
		case pgiExtendedUserData:
			// ISO 8327-1: Extended User Data is defined for CONNECT only.
			hasExtUD = true
			result.UserData = val
		case pgiConnAcceptItem:
			return walkParams(val, result.Type, func(c byte, v []byte) error {
				return parseAcceptItem(c, v, result)
			})
		case piVersion, piProtocolOpts:
			// Some encoders place these PIs outside PGI 5; read them there too.
			return parseAcceptItem(code, val, result)
		case piSessionUserReq:
			if len(val) != 2 {
				return fmt.Errorf("session: %s Session User Requirements length %d, want 2", result.Type, len(val))
			}
			result.HasRequirements = true
			result.Requirements = binary.BigEndian.Uint16(val)
		default:
			// Interop: skip unknown session parameters. ISO 8327 defines
			// parameters beyond those used by MMS; rejecting them would
			// break interop.
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if hasUD && hasExtUD {
		return nil, fmt.Errorf("session: %s SPDU has both User Data and Extended User Data", result.Type)
	}
	return result, nil
}

func parseAcceptItem(code byte, val []byte, result *ParsedSpdu) error {
	if code == piVersion {
		if len(val) != 1 {
			return fmt.Errorf("session: %s Version Number length %d, want 1", result.Type, len(val))
		}
		result.Version = val[0]
	}
	return nil
}

func parseSimpleWithUserData(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	params, err := header(data, result.Type)
	if err != nil {
		return nil, err
	}
	err = walkParams(params, result.Type, func(code byte, val []byte) error {
		if code == pgiUserData {
			result.UserData = val
		}
		// Interop: skip unknown session parameters (Transport
		// Disconnect, Enclosure Item, ...).
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func parseRefuse(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	params, err := header(data, result.Type)
	if err != nil {
		return nil, err
	}
	err = walkParams(params, result.Type, func(code byte, val []byte) error {
		switch code {
		case piReasonCode:
			if len(val) == 0 {
				return fmt.Errorf("session: REFUSE Reason Code empty")
			}
			result.HasReason = true
			result.Reason = val[0]
			if len(val) > 1 {
				result.UserData = val[1:]
			}
		case piVersion:
			return parseAcceptItem(code, val, result)
		case piSessionUserReq:
			if len(val) == 2 {
				result.HasRequirements = true
				result.Requirements = binary.BigEndian.Uint16(val)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// NegotiateVersion selects the protocol version for an ACCEPT from the
// bit mask a CONNECT proposed: version 2 when offered, else version 1.
// An absent Version Number (0) means version 1 (ISO 8327-1 8.3.1.7). It
// returns 0 when neither version this implementation supports was
// proposed.
func NegotiateVersion(proposed byte) byte {
	switch {
	case proposed == 0:
		return Version1
	case proposed&Version2 != 0:
		return Version2
	case proposed&Version1 != 0:
		return Version1
	default:
		return 0
	}
}

// maxLI is the largest length the LI field can carry (ISO 8327-1 LI field).
const maxLI = 0xffff

// liSize returns the size of the LI field for length l.
func liSize(l int) int {
	if l < 0xff {
		return 1
	}
	return 3
}

// appendLI appends an LI field: one octet below 255, else 0xff and two
// octets. Callers bound l to maxLI; larger values are clamped.
func appendLI(buf []byte, l int) []byte {
	if l >= 0 && l < 0xff {
		return append(buf, byte(l))
	}
	return binary.BigEndian.AppendUint16(append(buf, 0xff), uint16(min(max(l, 0), maxLI)))
}

// appendPGI appends a PGI or PI item to buf: code + LI + content. Content
// longer than maxLI is rejected by encodeSPDU, since the SPDU that holds
// it is longer still.
func appendPGI(buf []byte, tag byte, content []byte) []byte {
	buf = append(buf, tag)
	buf = appendLI(buf, len(content))
	return append(buf, content...)
}

// decodePGILength decodes the length field of a PGI/PI item, returning
// the content length and how many bytes the length field itself consumed.
func decodePGILength(data []byte) (length int, consumed int, err error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("missing length byte")
	}
	if data[0] != 0xff {
		return int(data[0]), 1, nil
	}
	// Extended form: 0xff + 2-byte big-endian length
	if len(data) < 3 {
		return 0, 0, fmt.Errorf("truncated extended length")
	}
	return int(binary.BigEndian.Uint16(data[1:3])), 3, nil
}
