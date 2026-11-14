// SPDX-License-Identifier: MIT

package iso

import "math"

// The largest TSDU a connection accepts and sends (go-cotp MaxTSDULength).
// go-cotp refuses a longer received TSDU before it holds it, and ends the
// connection; it refuses to send a longer one before any I/O.
//
// A TSDU carries one session SPDU. Every SPDU but DATA carries its user
// data in parameters, whose length the LI field bounds to 65535 octets
// (ISO 8327-1), so it is at most maxSessionSPDU octets: CONNECT with the
// AARQ, ACCEPT with the AARE, FINISH and DISCONNECT with the release, and
// REFUSE and ABORT. A DATA SPDU carries one MMS PDU and dataFraming octets
// more. The MMS PDU is at most the size a side proposes or supports
// (ISO 9506-2 clause 8.2: localDetailCalling and localDetailCalled), so
// the bound is the larger of maxSessionSPDU and that size plus
// dataFraming: 65,539 octets for the default PDU size of 65,000.
const (
	// maxSessionSPDU is SI, a three-octet LI and 65535 octets of
	// parameters.
	maxSessionSPDU = 1 + 3 + 0xffff

	// dataFraming bounds what a DATA TSDU adds to its MMS PDU: the
	// session Give Tokens and Data Transfer headers (4 octets) and the
	// presentation fully-encoded-data (ISO 8823-1) around the PDU, three
	// tags with lengths of up to five octets each and the context
	// identifier, 22 octets for a PDU below 2^32 octets. The rest is room
	// for an optional transfer-syntax-name.
	dataFraming = 64

	// defaultMMSPDUSize is the MMS PDU size a client proposes and a server
	// supports when mms.MMSOptions.MaxPDUSize or
	// mms.ServerMMSOptions.MaxPDUSize is zero.
	defaultMMSPDUSize = 65000
)

// maxTSDULength returns the TSDU bound of a connection that carries MMS
// PDUs of up to maxPDU octets (defaultMMSPDUSize when zero or negative).
func maxTSDULength(maxPDU int) int {
	if maxPDU <= 0 {
		maxPDU = defaultMMSPDUSize
	}
	if maxPDU > math.MaxInt-dataFraming {
		return math.MaxInt
	}
	return max(maxSessionSPDU, maxPDU+dataFraming)
}

// MaxMMSPDU returns the largest MMS PDU the transport carries in a DATA
// TSDU. The mms package reads it to propose and support no larger PDU
// size than the transport carries.
func (t *cotpTransport) MaxMMSPDU() int {
	return t.maxTSDU - dataFraming
}
