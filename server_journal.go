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

	var result *JournalResult
	switch {
	case req.IsTimeRange:
		result, err = s.journalProvider.ReadTimeRange(ctx, req.Domain, req.Journal,
			req.StartTime, req.StopTime, defaultMaxJournalEntries)
	case req.IsStartAfter:
		result, err = s.journalProvider.ReadStartAfter(ctx, req.Domain, req.Journal,
			req.AfterID, req.AfterTime, defaultMaxJournalEntries)
	default:
		return 0, false, nil, errInvalidRequest
	}

	if err != nil {
		return 0, false, nil, journalError(err)
	}

	wireEntries, wErr := journalEntriesToWire(result.Entries)
	if wErr != nil {
		return 0, false, nil, fmt.Errorf("marshal journal entries: %w", wErr)
	}

	payload, mErr := pdu.MarshalReadJournalResponse(wireEntries, result.MoreFollows)
	if mErr != nil {
		return 0, false, nil, fmt.Errorf("marshal read-journal response: %w", mErr)
	}

	return asn1util.TagNumReadJournal, true, payload, nil
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
		}
	}
	return wire, nil
}
