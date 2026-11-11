// SPDX-License-Identifier: MIT

package mms

import (
	"context"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

// Negotiated dataStructureNestingLevel (ISO 9506-2 clause 8.2, Initiate).
//
// The level counts structure and array levels: a simple value has
// nesting 0, a structure or array of simple values nesting 1. The same
// count applies to a Data value, a TypeSpecification and an
// AlternateAccess chain (one level per selection). The server applies
// it to the listOfData of a Write (Reject confirmed-requestPDU
// max-recursion-exceeded); the client to what it sends and receives.

// dataNesting is the number of structure/array levels of dv.
func dataNesting(dv *pdu.DataValue) int {
	if dv == nil || (dv.Tag != pdu.TagDataArray && dv.Tag != pdu.TagDataStructure) {
		return 0
	}
	deepest := 0
	for _, e := range dv.Elements {
		deepest = max(deepest, dataNesting(e))
	}
	return deepest + 1
}

// dataListNesting is the deepest nesting of dvs.
func dataListNesting(dvs []*pdu.DataValue) int {
	deepest := 0
	for _, dv := range dvs {
		deepest = max(deepest, dataNesting(dv))
	}
	return deepest
}

// typeSpecNesting is the number of structure/array levels of ts. A
// typeName reference counts as a simple type: its definition is not part
// of the PDU.
func typeSpecNesting(ts *pdu.TypeSpecWire) int {
	switch {
	case ts == nil:
		return 0
	case ts.Tag == typeSpecArray:
		return typeSpecNesting(ts.Element) + 1
	case ts.Tag == typeSpecStructure:
		deepest := 0
		for i := range ts.Components {
			deepest = max(deepest, typeSpecNesting(&ts.Components[i].Type))
		}
		return deepest + 1
	}
	return 0
}

// nestingLimit is the negotiated dataStructureNestingLevel; 0 (none
// negotiated, or a server that answered 0) is not enforced, as on the
// server side.
func (c *Client) nestingLimit() int {
	return c.nestingLevel
}

// checkSendNesting refuses a request whose Data is nested deeper than
// the negotiated level: the server would reject it (confirmed-requestPDU
// max-recursion-exceeded, ISO 9506-2 clause 8.6). Nothing is sent.
func (c *Client) checkSendNesting(what string, dvs ...*pdu.DataValue) error {
	limit := c.nestingLimit()
	if limit <= 0 {
		return nil
	}
	if n := dataListNesting(dvs); n > limit {
		return invalidArgf("mms: %s: data nesting %d exceeds the negotiated dataStructureNestingLevel %d", what, n, limit)
	}
	return nil
}

// checkAccessNesting refuses an AlternateAccess chain with more
// selections than the negotiated nesting level: each selection descends
// one structure or array level, so a longer chain selects nothing the
// server may hold. Nothing is sent.
func (c *Client) checkAccessNesting(what string, index int, sels []AccessSelector) error {
	limit := c.nestingLimit()
	if limit > 0 && len(sels) > limit {
		return invalidArgf("mms: %s: variable [%d]: alternate access nesting %d exceeds the negotiated dataStructureNestingLevel %d", what, index, len(sels), limit)
	}
	return nil
}

// checkResponseNesting fails a confirmed response whose Data or
// TypeSpecification is nested deeper than the negotiated level with a
// decode error, and rejects the response (ISO 9506-2 clause 6.4;
// clause 8.6 confirmed-responsePDU max-recursion-exceeded). The
// association continues.
func (c *Client) checkResponseNesting(ctx context.Context, invokeID codec.InvokeID, what string, depth int) error {
	limit := c.nestingLimit()
	if limit <= 0 || depth <= limit {
		return nil
	}
	c.logger.Warn("mms: rejecting response nested deeper than negotiated",
		"invoke_id", invokeID, "service", what, "nesting", depth, "max", limit)
	c.sendReject(ctx, codec.MarshalRejectPDU(invokeID, int(RejectConfirmedResponse), rejectResponseMaxRecursion))
	return decodeErrorf("mms: %s response: nesting %d exceeds the negotiated dataStructureNestingLevel %d", what, depth, limit)
}

// TypeSpecification array [1] and structure [2] (ISO 9506-2 clause
// 14.2.1).
const (
	typeSpecArray     = 1
	typeSpecStructure = 2
)

// RejectPDU rejectReason values for max-recursion-exceeded (ISO 9506-2
// clause 8.6).
const (
	rejectResponseMaxRecursion    = 5 // confirmed-responsePDU
	rejectUnconfirmedMaxRecursion = 3 // unconfirmedPDU
)
