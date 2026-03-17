// Package session implements ISO 8327-1 session layer SPDU construction
// and parsing for the MMS protocol stack.
//
// Only the subset of session services needed by MMS is implemented:
// CONNECT, ACCEPT, DATA, FINISH, DISCONNECT, ABORT, and REFUSE SPDUs.
// This is internal protocol plumbing — users interact with mms.Client.
package session

import (
	"encoding/binary"
	"fmt"
)

// SPDU type identifiers (SI — Session Identifier).
const (
	SIData       byte = 0x01 // Give Tokens / Data Transfer
	SINotFinish  byte = 0x08
	SIFinish     byte = 0x09
	SIDisconnect byte = 0x0a
	SIRefuse     byte = 0x0c
	SIConnect    byte = 0x0d
	SIAccept     byte = 0x0e
	SIAbort      byte = 0x19
)

// Parameter Group / Parameter Identifier values.
const (
	pgiConnectionID    byte = 0x01
	pgiConnAcceptItem  byte = 0x05
	pgiTransportDisc   byte = 0x11
	pgiSessionUserReq  byte = 0x14
	pgiCallingSelector byte = 0x33
	pgiCalledSelector  byte = 0x34
	pgiUserData        byte = 0xc1

	piProtocolOpts byte = 0x13
	piVersion      byte = 0x16
)

// SpduType classifies a parsed SPDU.
type SpduType byte

const (
	SpduConnect    SpduType = SpduType(SIConnect)
	SpduAccept     SpduType = SpduType(SIAccept)
	SpduData       SpduType = SpduType(SIData)
	SpduFinish     SpduType = SpduType(SIFinish)
	SpduDisconnect SpduType = SpduType(SIDisconnect)
	SpduAbort      SpduType = SpduType(SIAbort)
	SpduRefuse     SpduType = SpduType(SIRefuse)
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
	default:
		return fmt.Sprintf("SpduType(0x%02x)", byte(t))
	}
}

// ConnectParams holds session-layer parameters for CONNECT and ACCEPT SPDUs.
type ConnectParams struct {
	CallingSelector []byte // calling session selector (max 16 bytes)
	CalledSelector  []byte // called session selector (max 16 bytes)
}

// ParsedSpdu is the result of parsing a session PDU.
type ParsedSpdu struct {
	Type            SpduType
	CallingSelector []byte
	CalledSelector  []byte
	UserData        []byte // slice into the original input; not copied
}

// EncodeConnect builds a CONNECT SPDU (SI=0x0d) with the given parameters
// and user data (the presentation layer payload).
func EncodeConnect(p ConnectParams, userData []byte) []byte {
	return encodeConnectAccept(SIConnect, p.CallingSelector, p.CalledSelector, userData)
}

// EncodeAccept builds an ACCEPT SPDU (SI=0x0e) with the given parameters
// and user data.
func EncodeAccept(p ConnectParams, userData []byte) []byte {
	return encodeConnectAccept(SIAccept, p.CallingSelector, p.CalledSelector, userData)
}

func encodeConnectAccept(si byte, calling, called, userData []byte) []byte {
	// Connection/Accept Item: PI 19 (protocol opts) + PI 22 (version=2)
	connAccept := []byte{
		piProtocolOpts, 0x01, 0x00, // protocol options = 0
		piVersion, 0x01, 0x02, // version = 2
	}

	// Calculate total parameter length.
	paramLen := 2 + len(connAccept) // PGI 5 header + content
	paramLen += 4                   // PGI 20 session user requirements (2+2)

	if len(calling) > 0 {
		paramLen += 2 + len(calling) // PGI 51 + len + data
	}
	if len(called) > 0 {
		paramLen += 2 + len(called) // PGI 52 + len + data
	}
	if len(userData) > 0 {
		paramLen += pgiLength(len(userData)) // PGI 193 + encoded length + data
	}

	buf := make([]byte, 0, 2+paramLen)
	buf = append(buf, si, byte(paramLen))

	// PGI 5 — Connection/Accept Item
	buf = append(buf, pgiConnAcceptItem, byte(len(connAccept)))
	buf = append(buf, connAccept...)

	// PGI 20 — Session User Requirements (duplex functional unit = 0x0002)
	buf = append(buf, pgiSessionUserReq, 0x02, 0x00, 0x02)

	// PGI 51 — Calling Session Selector
	if len(calling) > 0 {
		buf = append(buf, pgiCallingSelector, byte(len(calling)))
		buf = append(buf, calling...)
	}

	// PGI 52 — Called Session Selector
	if len(called) > 0 {
		buf = append(buf, pgiCalledSelector, byte(len(called)))
		buf = append(buf, called...)
	}

	// PGI 193 — User Data
	if len(userData) > 0 {
		buf = appendPGI(buf, pgiUserData, userData)
	}

	return buf
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
func EncodeFinish(userData []byte) []byte {
	return encodeSimpleWithUserData(SIFinish, userData)
}

// EncodeDisconnect builds a DISCONNECT SPDU (SI=0x0a) carrying user data.
func EncodeDisconnect(userData []byte) []byte {
	return encodeSimpleWithUserData(SIDisconnect, userData)
}

func encodeSimpleWithUserData(si byte, userData []byte) []byte {
	innerLen := pgiLength(len(userData))
	buf := make([]byte, 0, 2+innerLen)
	buf = append(buf, si, byte(innerLen))
	buf = appendPGI(buf, pgiUserData, userData)
	return buf
}

// EncodeAbort builds an ABORT SPDU (SI=0x19).
func EncodeAbort(userData []byte) []byte {
	transportDisc := []byte{pgiTransportDisc, 0x01, 0x0b} // transport disconnect = 11
	innerLen := len(transportDisc)
	if len(userData) > 0 {
		innerLen += pgiLength(len(userData))
	}

	buf := make([]byte, 0, 2+innerLen)
	buf = append(buf, SIAbort, byte(innerLen))
	buf = append(buf, transportDisc...)
	if len(userData) > 0 {
		buf = appendPGI(buf, pgiUserData, userData)
	}
	return buf
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
	case SIFinish, SIDisconnect:
		return parseSimpleWithUserData(data, result)
	case SIAbort:
		return parseAbort(data, result)
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

func parseConnectAccept(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	li := int(data[1])
	headerEnd := 2 + li
	if len(data) < headerEnd {
		return nil, fmt.Errorf("session: %s SPDU truncated (need %d, have %d)", result.Type, headerEnd, len(data))
	}

	offset := 2
	for offset < headerEnd {
		if offset+2 > headerEnd {
			break
		}
		pgi := data[offset]
		pgiLen, lenBytes, err := decodePGILength(data[offset+1:])
		if err != nil {
			return nil, fmt.Errorf("session: %s SPDU parameter length: %w", result.Type, err)
		}
		offset += 1 + lenBytes // skip PGI byte + length bytes
		end := offset + pgiLen
		if end > len(data) {
			return nil, fmt.Errorf("session: %s SPDU parameter 0x%02x truncated", result.Type, pgi)
		}

		switch pgi {
		case pgiCallingSelector:
			result.CallingSelector = data[offset:end]
		case pgiCalledSelector:
			result.CalledSelector = data[offset:end]
		case pgiUserData:
			result.UserData = data[offset:end]
		default:
			// Interop: skip unknown session PGIs. ISO 8327 defines PGIs
			// beyond those used by MMS; rejecting them would break interop.
		}
		offset = end
	}
	if offset != headerEnd {
		return nil, fmt.Errorf("session: %s SPDU %d trailing bytes in header", result.Type, headerEnd-offset)
	}
	return result, nil
}

func parseSimpleWithUserData(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	li := int(data[1])
	if len(data) < 2+li {
		return nil, fmt.Errorf("session: %s SPDU truncated", result.Type)
	}

	offset := 2
	end := 2 + li
	for offset < end {
		if offset+2 > end {
			break
		}
		pgi := data[offset]
		pgiLen, lenBytes, err := decodePGILength(data[offset+1:])
		if err != nil {
			return nil, fmt.Errorf("session: %s SPDU parameter length: %w", result.Type, err)
		}
		offset += 1 + lenBytes
		pEnd := offset + pgiLen
		if pEnd > len(data) {
			return nil, fmt.Errorf("session: %s SPDU parameter 0x%02x truncated", result.Type, pgi)
		}
		switch pgi {
		case pgiUserData:
			result.UserData = data[offset:pEnd]
		default:
			// Interop: skip unknown session PGIs. ISO 8327 defines PGIs
			// beyond those used by MMS; rejecting them would break interop.
		}
		offset = pEnd
	}
	if offset != end {
		return nil, fmt.Errorf("session: %s SPDU %d trailing bytes", result.Type, end-offset)
	}
	return result, nil
}

func parseAbort(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	return parseSimpleWithUserData(data, result)
}

func parseRefuse(data []byte, result *ParsedSpdu) (*ParsedSpdu, error) {
	li := int(data[1])
	if len(data) < 2+li {
		return nil, fmt.Errorf("session: REFUSE SPDU truncated")
	}
	return result, nil
}

// pgiLength returns the total encoded size of a PGI item (tag + length + content).
func pgiLength(contentLen int) int {
	if contentLen < 0xff {
		return 2 + contentLen // tag(1) + len(1) + content
	}
	return 4 + contentLen // tag(1) + 0xff(1) + len(2) + content
}

// appendPGI appends a PGI item to buf: tag + length + content.
func appendPGI(buf []byte, tag byte, content []byte) []byte {
	l := len(content)
	if l < 0xff {
		buf = append(buf, tag, byte(l))
	} else {
		buf = append(buf, tag, 0xff)
		buf = binary.BigEndian.AppendUint16(buf, uint16(l))
	}
	buf = append(buf, content...)
	return buf
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
