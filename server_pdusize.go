// SPDX-License-Identifier: MIT

package mms

import (
	"context"
)

// confirmedResponseOverhead bounds the confirmed-response framing around
// a service payload: [1] tag and length (4), invokeID (6) and the
// service tag and length (5).
const confirmedResponseOverhead = 16

// errResponseTooLarge is returned when not even one list element fits
// the negotiated PDU size.
var errResponseTooLarge = errResourceOther

// responseBudget is the largest service payload that fits the PDU size
// negotiated on the connection serving ctx.
func (s *Server) responseBudget(ctx context.Context) int {
	maxPDU := s.mmsOpts.MaxPDUSize
	if sc := serverConnFromCtx(ctx); sc != nil && sc.conn != nil {
		maxPDU = sc.conn.MaxPDUSize()
	}
	return maxPDU - confirmedResponseOverhead
}

// fitList encodes the longest prefix of an n-element list whose encoding
// fits budget. encode(k, truncated) must encode the first k elements and
// set moreFollows when truncated is true.
func fitList(n, budget int, encode func(k int, truncated bool) ([]byte, error)) ([]byte, error) {
	full, err := encode(n, false)
	if err != nil || len(full) <= budget {
		return full, err
	}
	var best []byte
	lo, hi := 1, n-1
	for lo <= hi {
		mid := (lo + hi) / 2
		b, err := encode(mid, true)
		if err != nil {
			return nil, err
		}
		if len(b) <= budget {
			best = b
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best == nil {
		return nil, errResponseTooLarge
	}
	return best, nil
}
