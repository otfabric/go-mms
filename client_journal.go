// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

// JournalEntry is a single entry returned by a ReadJournal call.
// These types are shared between client results and server-side
// [JournalProvider] implementations; the journal entry shape is
// defined by the MMS protocol and is the same on both sides.
type JournalEntry struct {
	EntryID        []byte
	OccurrenceTime time.Time
	Variables      []JournalVariable

	// Annotation holds the text of an annotation entry (ISO 9506-2
	// clause 23.8.1 entryForm annotation [3]) when HasAnnotation is set;
	// such an entry has no Variables.
	HasAnnotation bool
	Annotation    string
}

// JournalVariable is one variable inside a journal entry.
type JournalVariable struct {
	Tag   string
	Value *Value
}

// JournalResult holds the result of a ReadJournal call, including
// continuation state for paging. When MoreFollows is true, use
// [Client.ReadJournalStartAfter] with the last entry's OccurrenceTime
// and EntryID to retrieve the next page.
type JournalResult struct {
	Entries     []JournalEntry
	MoreFollows bool
}

// ReadJournalTimeRange reads journal entries from the remote server
// within the given time range [start, stop].
func (c *Client) ReadJournalTimeRange(ctx context.Context, domain, journal string, start, stop time.Time) (*JournalResult, error) {
	invokeID := c.nextInvokeID()
	reqBytes, err := pdu.MarshalReadJournalTimeRange(invokeID, domain, journal, start, stop)
	if err != nil {
		return nil, invalidArgf("mms: marshal read-journal request: %w", err)
	}
	return c.sendJournalRequest(ctx, invokeID, reqBytes)
}

// ReadJournalStartAfter reads journal entries from the remote server
// starting after the given (afterTime, afterID) cursor. Use the last
// entry's OccurrenceTime and EntryID from a previous result to page
// through journal data.
func (c *Client) ReadJournalStartAfter(ctx context.Context, domain, journal string, afterTime time.Time, afterID []byte) (*JournalResult, error) {
	invokeID := c.nextInvokeID()
	reqBytes, err := pdu.MarshalReadJournalStartAfter(invokeID, domain, journal, afterTime, afterID)
	if err != nil {
		return nil, invalidArgf("mms: marshal read-journal request: %w", err)
	}
	return c.sendJournalRequest(ctx, invokeID, reqBytes)
}

func (c *Client) sendJournalRequest(ctx context.Context, invokeID codec.InvokeID, reqBytes []byte) (*JournalResult, error) {
	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceReadJournal {
		return nil, &ProtocolError{
			Phase:   PhaseMMS,
			Message: fmt.Sprintf("expected ReadJournal response, got %s", confirmed.ServiceKind),
		}
	}

	wireEntries, moreFollows, err := pdu.UnmarshalReadJournalResponse(confirmed.ServiceData.Bytes)
	if err != nil {
		return nil, decodeErrorf("mms: %w", err)
	}
	depth := 0
	for _, we := range wireEntries {
		for _, wv := range we.Variables {
			depth = max(depth, dataNesting(wv.Value))
		}
	}
	if err := c.checkResponseNesting(ctx, invokeID, "readjournal", depth); err != nil {
		return nil, err
	}

	entries := make([]JournalEntry, len(wireEntries))
	for i, we := range wireEntries {
		vars := make([]JournalVariable, len(we.Variables))
		for j, wv := range we.Variables {
			val, vErr := dataValueToValue(wv.Value)
			if vErr != nil {
				return nil, decodeErrorf("mms: journal entry %d variable %q: %w", i, wv.Tag, vErr)
			}
			vars[j] = JournalVariable{Tag: wv.Tag, Value: val}
		}
		entries[i] = JournalEntry{
			EntryID:        we.EntryID,
			OccurrenceTime: we.OccurrenceTime,
			Variables:      vars,
			HasAnnotation:  we.HasAnnotation,
			Annotation:     we.Annotation,
		}
	}

	return &JournalResult{
		Entries:     entries,
		MoreFollows: moreFollows,
	}, nil
}
