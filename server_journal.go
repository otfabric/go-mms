// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/serverconn"
)

// JournalProvider is implemented by applications to back journal services.
// The server calls these methods in response to ReadJournal requests.
//
// The provider methods use the same domain types as the client API
// ([JournalEntry], [JournalVariable], [JournalResult]). This is an
// intentional design choice: journal entries have a fixed structure
// defined by the MMS protocol, so a single set of types serves both
// sides without loss of fidelity.
//
// # Ordering contract
//
// Entries returned by ReadTimeRange and ReadStartAfter MUST be in
// ascending chronological order by OccurrenceTime. When multiple entries
// share the same OccurrenceTime, they MUST be in a stable, deterministic
// order (e.g. by EntryID). ReadStartAfter semantics depend on this
// ordering: the server sends the client the last entry's
// (OccurrenceTime, EntryID) as the continuation cursor.
//
// # Pagination
//
// The provider MAY return fewer than maxEntries entries. When
// [JournalResult.MoreFollows] is true, the caller should issue a
// ReadStartAfter request using the last returned entry's OccurrenceTime
// and EntryID to retrieve the next page. The server passes
// defaultMaxJournalEntries (currently 100) as maxEntries.
//
// # Error mapping
//
// If the provider returns [fs.ErrNotExist], the server maps it to MMS
// error class "access" (7), code "object-non-existent" (0). If the
// provider returns [fs.ErrPermission] or [ErrFileAccessDenied], the
// server maps it to "access" (7), code "object-access-denied" (1). All
// other errors are mapped to "access" (7), code "other" (0).
type JournalProvider interface {
	// ListJournals returns journal names available in the given domain.
	// Currently only domain-scoped journals are supported in the
	// server dispatch (ObjectClassJournal + ScopeDomain).
	ListJournals(ctx context.Context, domain string) ([]string, error)

	// ReadTimeRange returns journal entries within [start, stop].
	// maxEntries is a hint; the provider may return fewer entries.
	// Set [JournalResult.MoreFollows] to true if more entries are
	// available beyond those returned.
	ReadTimeRange(ctx context.Context, domain, journal string,
		start, stop time.Time, maxEntries int) (*JournalResult, error)

	// ReadStartAfter returns journal entries starting after the given
	// cursor (afterID, afterTime). Use the last entry's EntryID and
	// OccurrenceTime from a previous result to page through data.
	// maxEntries is a hint; the provider may return fewer entries.
	// Set [JournalResult.MoreFollows] to true if more entries remain.
	ReadStartAfter(ctx context.Context, domain, journal string,
		afterID []byte, afterTime time.Time, maxEntries int) (*JournalResult, error)
}

// JournalQuery is a complete ReadJournal request (ISO 9506-2 clause
// 23.2). IEC 61850-8-1 Table 124 requires a server to support every
// option; the start and stop specifications are independent.
type JournalQuery struct {
	Domain, Journal string

	// Range start: StartTime (startingTime) or StartEntry (startingEntry,
	// inclusive). Neither set: from the oldest entry.
	HasStartTime bool
	StartTime    time.Time
	StartEntry   []byte

	// Range stop: StopTime (endingTime) or NumberOfEntries. Neither set:
	// to the newest entry.
	HasStopTime        bool
	StopTime           time.Time
	HasNumberOfEntries bool
	NumberOfEntries    int

	// entryToStartAfter: return entries after (AfterTime, AfterID).
	HasStartAfter bool
	AfterTime     time.Time
	AfterID       []byte

	// ListOfVariables restricts the variables returned per entry; nil
	// returns all.
	ListOfVariables []string

	// MaxEntries is the number of entries the server can send; the
	// provider may return fewer and set MoreFollows.
	MaxEntries int
}

// JournalQueryProvider is an optional extension of [JournalProvider]: a
// provider that implements it receives every ReadJournal request in full.
// Without it the server maps a request onto ReadTimeRange and
// ReadStartAfter, filtering and truncating the result; a startingEntry
// range start then cannot be served and is answered with
// access/object-access-unsupported.
type JournalQueryProvider interface {
	JournalProvider
	ReadJournal(ctx context.Context, q JournalQuery) (*JournalResult, error)
}

// journalOpenEnd is the stop time used for an open-ended range.
var journalOpenEnd = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// readJournal serves q from the provider (MMS-PDU-12).
func (s *Server) readJournal(ctx context.Context, q JournalQuery) (*JournalResult, error) {
	if qp, ok := s.journalProvider.(JournalQueryProvider); ok {
		return qp.ReadJournal(ctx, q)
	}
	limit := q.MaxEntries
	if q.HasNumberOfEntries && q.NumberOfEntries < limit {
		limit = q.NumberOfEntries
	}
	var (
		res *JournalResult
		err error
	)
	switch {
	case q.StartEntry != nil && !q.HasStartAfter:
		return nil, errAccessUnsupported
	case q.HasStartAfter:
		res, err = s.journalProvider.ReadStartAfter(ctx, q.Domain, q.Journal, q.AfterID, q.AfterTime, limit)
	default:
		start, stop := time.Time{}, journalOpenEnd
		if q.HasStartTime {
			start = q.StartTime
		}
		if q.HasStopTime {
			stop = q.StopTime
		}
		res, err = s.journalProvider.ReadTimeRange(ctx, q.Domain, q.Journal, start, stop, limit)
	}
	if err != nil || res == nil {
		return res, err
	}
	return filterJournal(res, q, limit), nil
}

// filterJournal applies the parts of q a legacy provider method does not
// take: the stop time and start time after a start-after cursor, the
// entry count and the variable list.
func filterJournal(res *JournalResult, q JournalQuery, limit int) *JournalResult {
	out := &JournalResult{MoreFollows: res.MoreFollows}
	var keep map[string]bool
	if q.ListOfVariables != nil {
		keep = make(map[string]bool, len(q.ListOfVariables))
		for _, v := range q.ListOfVariables {
			keep[v] = true
		}
	}
	for _, e := range res.Entries {
		if q.HasStartAfter && q.HasStartTime && e.OccurrenceTime.Before(q.StartTime) {
			continue
		}
		if q.HasStopTime && e.OccurrenceTime.After(q.StopTime) {
			out.MoreFollows = false
			break
		}
		if len(out.Entries) >= limit {
			out.MoreFollows = true
			break
		}
		if keep != nil {
			vars := make([]JournalVariable, 0, len(e.Variables))
			for _, v := range e.Variables {
				if keep[v.Tag] {
					vars = append(vars, v)
				}
			}
			e.Variables = vars
		}
		out.Entries = append(out.Entries, e)
	}
	return out
}

func journalError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errObjectNonExistent
	case errors.Is(err, fs.ErrPermission), errors.Is(err, ErrFileAccessDenied):
		return errAccessDenied
	default:
		return &serverconn.ServiceError{ErrorClass: serviceErrorClassAccess, ErrorCode: svcErrOther}
	}
}

const defaultMaxJournalEntries = 100

func (s *Server) handleReadJournal(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.journalProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	req, err := pdu.UnmarshalReadJournalRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	result, err := s.readJournal(ctx, JournalQuery{
		Domain: req.Domain, Journal: req.Journal,
		HasStartTime: req.HasStartTime, StartTime: req.StartTime, StartEntry: startEntry(req),
		HasStopTime: req.HasStopTime, StopTime: req.StopTime,
		HasNumberOfEntries: req.HasNumberOfEntries, NumberOfEntries: req.NumberOfEntries,
		HasStartAfter: req.IsStartAfter, AfterTime: req.AfterTime, AfterID: req.AfterID,
		ListOfVariables: req.ListOfVariables,
		MaxEntries:      defaultMaxJournalEntries,
	})
	if errors.Is(err, errAccessUnsupported) {
		return 0, false, nil, errAccessUnsupported
	}
	if err != nil {
		return 0, false, nil, journalError(err)
	}

	wireEntries, wErr := journalEntriesToWire(result.Entries)
	if wErr != nil {
		return 0, false, nil, fmt.Errorf("marshal journal entries: %w", wErr)
	}

	payload, mErr := fitList(len(wireEntries), s.responseBudget(ctx), func(k int, truncated bool) ([]byte, error) {
		return pdu.MarshalReadJournalResponse(wireEntries[:k], result.MoreFollows || truncated)
	})
	if errors.Is(mErr, errResponseTooLarge) {
		return 0, false, nil, mErr
	}
	if mErr != nil {
		return 0, false, nil, fmt.Errorf("marshal read-journal response: %w", mErr)
	}

	return asn1util.TagNumReadJournal, true, payload, nil
}

func startEntry(req *pdu.ReadJournalRequest) []byte {
	if !req.HasStartEntry {
		return nil
	}
	if req.StartEntry == nil {
		return []byte{}
	}
	return req.StartEntry
}

func journalEntriesToWire(entries []JournalEntry) ([]pdu.JournalEntryWire, error) {
	wire := make([]pdu.JournalEntryWire, len(entries))
	for i, e := range entries {
		vars := make([]pdu.JournalVariableWire, len(e.Variables))
		for j, v := range e.Variables {
			dv, err := valueToDataValue(v.Value)
			if err != nil {
				return nil, fmt.Errorf("entry %d variable %q: %w", i, v.Tag, err)
			}
			vars[j] = pdu.JournalVariableWire{Tag: v.Tag, Value: dv}
		}
		wire[i] = pdu.JournalEntryWire{
			EntryID:        e.EntryID,
			OccurrenceTime: e.OccurrenceTime,
			Variables:      vars,
			HasAnnotation:  e.HasAnnotation,
			Annotation:     e.Annotation,
		}
	}
	return wire, nil
}
