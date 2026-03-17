package mms

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- In-memory journal provider for tests ---

type memJournal struct {
	name    string
	entries []JournalEntry
}

type memJournalProvider struct {
	mu       sync.Mutex
	journals map[string]map[string]*memJournal // domain -> name -> journal
}

func newMemJournalProvider() *memJournalProvider {
	return &memJournalProvider{
		journals: make(map[string]map[string]*memJournal),
	}
}

func (p *memJournalProvider) addJournal(domain, name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.journals[domain] == nil {
		p.journals[domain] = make(map[string]*memJournal)
	}
	if p.journals[domain][name] == nil {
		p.journals[domain][name] = &memJournal{name: name}
	}
}

func (p *memJournalProvider) addEntry(domain, name string, entry JournalEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	j := p.journals[domain][name]
	j.entries = append(j.entries, entry)
}

func (p *memJournalProvider) ListJournals(_ context.Context, domain string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	domJournals := p.journals[domain]
	if domJournals == nil {
		return nil, nil
	}
	var names []string
	for n := range domJournals {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (p *memJournalProvider) ReadTimeRange(_ context.Context, domain, journal string,
	start, stop time.Time, maxEntries int) (*JournalResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	domJournals := p.journals[domain]
	if domJournals == nil {
		return nil, fs.ErrNotExist
	}
	j, ok := domJournals[journal]
	if !ok {
		return nil, fs.ErrNotExist
	}

	var matched []JournalEntry
	for _, e := range j.entries {
		if (e.OccurrenceTime.Equal(start) || e.OccurrenceTime.After(start)) &&
			(e.OccurrenceTime.Equal(stop) || e.OccurrenceTime.Before(stop)) {
			matched = append(matched, e)
		}
	}

	moreFollows := false
	if maxEntries > 0 && len(matched) > maxEntries {
		matched = matched[:maxEntries]
		moreFollows = true
	}

	return &JournalResult{Entries: matched, MoreFollows: moreFollows}, nil
}

func (p *memJournalProvider) ReadStartAfter(_ context.Context, domain, journal string,
	afterID []byte, afterTime time.Time, maxEntries int) (*JournalResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	domJournals := p.journals[domain]
	if domJournals == nil {
		return nil, fs.ErrNotExist
	}
	j, ok := domJournals[journal]
	if !ok {
		return nil, fs.ErrNotExist
	}

	// Find the starting position: after the entry with matching ID and time.
	startIdx := 0
	for i, e := range j.entries {
		if bytes.Equal(e.EntryID, afterID) && e.OccurrenceTime.Equal(afterTime) {
			startIdx = i + 1
			break
		}
	}

	var matched []JournalEntry
	if startIdx < len(j.entries) {
		matched = append(matched, j.entries[startIdx:]...)
	}

	moreFollows := false
	if maxEntries > 0 && len(matched) > maxEntries {
		matched = matched[:maxEntries]
		moreFollows = true
	}

	return &JournalResult{Entries: matched, MoreFollows: moreFollows}, nil
}

// --- Test setup ---

func journalTestSetup(t *testing.T, jp JournalProvider) (context.Context, context.CancelFunc, *Client) {
	t.Helper()
	srv := NewServer(ServerOptions{JournalProvider: jp})
	srv.HandleIdentify(testIdentifyHandler)
	srv.HandleStatus(testStatusHandler)

	cl, sr := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	go func() { _ = srv.Serve(ctx, sr) }()

	client, err := NewClient(ctx, cl, DialOptions{})
	if err != nil {
		cancel()
		t.Fatalf("NewClient: %v", err)
	}
	return ctx, cancel, client
}

// --- Integration tests ---

func TestReadJournalTimeRangeEndToEnd(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "EventLog")

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		jp.addEntry("TestDomain", "EventLog", JournalEntry{
			EntryID:        []byte{byte(i + 1)},
			OccurrenceTime: base.Add(time.Duration(i) * time.Minute),
			Variables: []JournalVariable{
				{Tag: "Value", Value: NewInteger(int64(i * 10))},
			},
		})
	}

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	start := base
	stop := base.Add(2 * time.Minute)
	result, err := client.ReadJournalTimeRange(ctx, "TestDomain", "EventLog", start, stop)
	if err != nil {
		t.Fatalf("ReadJournalTimeRange: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}
	if result.MoreFollows {
		t.Error("expected MoreFollows=false")
	}

	for i, e := range result.Entries {
		if !bytes.Equal(e.EntryID, []byte{byte(i + 1)}) {
			t.Errorf("entry[%d].EntryID = %v, want [%d]", i, e.EntryID, i+1)
		}
		if len(e.Variables) != 1 {
			t.Errorf("entry[%d] has %d variables, want 1", i, len(e.Variables))
			continue
		}
		if e.Variables[0].Tag != "Value" {
			t.Errorf("entry[%d].Variables[0].Tag = %q, want \"Value\"", i, e.Variables[0].Tag)
		}
		got, ok := e.Variables[0].Value.Int64()
		if !ok {
			t.Errorf("entry[%d].Variables[0].Value: not an integer", i)
			continue
		}
		want := int64(i * 10)
		if got != want {
			t.Errorf("entry[%d].Variables[0].Value = %d, want %d", i, got, want)
		}
	}
}

func TestReadJournalStartAfterEndToEnd(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "EventLog")

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		jp.addEntry("TestDomain", "EventLog", JournalEntry{
			EntryID:        []byte{byte(i + 1)},
			OccurrenceTime: base.Add(time.Duration(i) * time.Minute),
			Variables: []JournalVariable{
				{Tag: "Value", Value: NewInteger(int64(i))},
			},
		})
	}

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	// Read starting after entry 2 (ID=[2], time=base+1min)
	afterTime := base.Add(1 * time.Minute)
	afterID := []byte{2}

	result, err := client.ReadJournalStartAfter(ctx, "TestDomain", "EventLog", afterTime, afterID)
	if err != nil {
		t.Fatalf("ReadJournalStartAfter: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries (3,4,5), got %d", len(result.Entries))
	}
	if result.MoreFollows {
		t.Error("expected MoreFollows=false")
	}

	// Should be entries 3, 4, 5
	for i, e := range result.Entries {
		want := byte(i + 3)
		if !bytes.Equal(e.EntryID, []byte{want}) {
			t.Errorf("entry[%d].EntryID = %v, want [%d]", i, e.EntryID, want)
		}
	}
}

func TestJournalGetNameListEndToEnd(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "AlarmLog")
	jp.addJournal("TestDomain", "EventLog")

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassJournal,
		Scope:       ObjectScopeDomain,
		DomainID:    "TestDomain",
	})
	if err != nil {
		t.Fatalf("GetNameList(journal): %v", err)
	}
	if len(result.Names) != 2 {
		t.Fatalf("expected 2 journal names, got %d: %v", len(result.Names), result.Names)
	}
	if result.Names[0] != "AlarmLog" || result.Names[1] != "EventLog" {
		t.Errorf("journal names = %v, want [AlarmLog EventLog]", result.Names)
	}
}

func TestJournalNotConfigured(t *testing.T) {
	srv := NewServer(ServerOptions{})
	srv.HandleIdentify(testIdentifyHandler)
	srv.HandleStatus(testStatusHandler)

	cl, sr := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = srv.Serve(ctx, sr) }()

	client, err := NewClient(ctx, cl, DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close(ctx) }()

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	_, err = client.ReadJournalTimeRange(ctx, "TestDomain", "Log", base, base.Add(time.Hour))
	if err == nil {
		t.Fatal("expected error when JournalProvider is nil")
	}
}

func TestReadJournalNonExistentJournal(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "EventLog")

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	_, err := client.ReadJournalTimeRange(ctx, "TestDomain", "NoSuchLog", base, base.Add(time.Hour))
	if err == nil {
		t.Fatal("expected error for non-existent journal")
	}
}

func TestReadJournalEmptyResult(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "EventLog")

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	result, err := client.ReadJournalTimeRange(ctx, "TestDomain", "EventLog", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadJournalTimeRange: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestReadJournalMultipleVariables(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "EventLog")

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	jp.addEntry("TestDomain", "EventLog", JournalEntry{
		EntryID:        []byte{0x01},
		OccurrenceTime: base,
		Variables: []JournalVariable{
			{Tag: "Voltage", Value: NewFloat(230.5)},
			{Tag: "Current", Value: NewFloat(12.3)},
			{Tag: "Status", Value: NewInteger(1)},
		},
	})

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.ReadJournalTimeRange(ctx, "TestDomain", "EventLog", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadJournalTimeRange: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	if len(result.Entries[0].Variables) != 3 {
		t.Fatalf("expected 3 variables, got %d", len(result.Entries[0].Variables))
	}
	if result.Entries[0].Variables[0].Tag != "Voltage" {
		t.Errorf("variable[0].Tag = %q, want Voltage", result.Entries[0].Variables[0].Tag)
	}
	if result.Entries[0].Variables[2].Tag != "Status" {
		t.Errorf("variable[2].Tag = %q, want Status", result.Entries[0].Variables[2].Tag)
	}
}

func TestReadJournalPaginationEndToEnd(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "BigLog")

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	total := 5
	for i := 0; i < total; i++ {
		jp.addEntry("TestDomain", "BigLog", JournalEntry{
			EntryID:        []byte{byte(i + 1)},
			OccurrenceTime: base.Add(time.Duration(i) * time.Minute),
			Variables: []JournalVariable{
				{Tag: "Seq", Value: NewInteger(int64(i))},
			},
		})
	}

	// Use a provider wrapper that limits to 2 entries per call.
	paged := &pagingJournalProvider{inner: jp, pageSize: 2}

	ctx, cancel, client := journalTestSetup(t, paged)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	// First request: time range, should get 2 entries + moreFollows=true
	result, err := client.ReadJournalTimeRange(ctx, "TestDomain", "BigLog",
		base, base.Add(time.Duration(total)*time.Minute))
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("page 1: expected 2 entries, got %d", len(result.Entries))
	}
	if !result.MoreFollows {
		t.Fatal("page 1: expected MoreFollows=true")
	}

	// Collect all entries via paging.
	var all []JournalEntry
	all = append(all, result.Entries...)

	for result.MoreFollows {
		last := result.Entries[len(result.Entries)-1]
		result, err = client.ReadJournalStartAfter(ctx, "TestDomain", "BigLog",
			last.OccurrenceTime, last.EntryID)
		if err != nil {
			t.Fatalf("continuation: %v", err)
		}
		all = append(all, result.Entries...)
	}

	if len(all) != total {
		t.Fatalf("expected %d total entries, got %d", total, len(all))
	}
	for i, e := range all {
		if !bytes.Equal(e.EntryID, []byte{byte(i + 1)}) {
			t.Errorf("entry[%d].EntryID = %v, want [%d]", i, e.EntryID, i+1)
		}
	}
}

// pagingJournalProvider wraps a memJournalProvider to enforce a page size.
type pagingJournalProvider struct {
	inner    *memJournalProvider
	pageSize int
}

func (p *pagingJournalProvider) ListJournals(ctx context.Context, domain string) ([]string, error) {
	return p.inner.ListJournals(ctx, domain)
}

func (p *pagingJournalProvider) ReadTimeRange(ctx context.Context, domain, journal string,
	start, stop time.Time, maxEntries int) (*JournalResult, error) {
	limit := p.pageSize
	if maxEntries > 0 && maxEntries < limit {
		limit = maxEntries
	}
	return p.inner.ReadTimeRange(ctx, domain, journal, start, stop, limit)
}

func (p *pagingJournalProvider) ReadStartAfter(ctx context.Context, domain, journal string,
	afterID []byte, afterTime time.Time, maxEntries int) (*JournalResult, error) {
	limit := p.pageSize
	if maxEntries > 0 && maxEntries < limit {
		limit = maxEntries
	}
	return p.inner.ReadStartAfter(ctx, domain, journal, afterID, afterTime, limit)
}

func TestReadJournalPermissionDenied(t *testing.T) {
	jp := &errorJournalProvider{err: fs.ErrPermission}
	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	_, err := client.ReadJournalTimeRange(ctx, "TestDomain", "Log", base, base.Add(time.Hour))
	if err == nil {
		t.Fatal("expected error for permission denied")
	}
	// The error should surface as a service error (access class)
	if !strings.Contains(err.Error(), "service error") {
		t.Errorf("expected service error, got: %v", err)
	}
}

func TestReadJournalGenericError(t *testing.T) {
	jp := &errorJournalProvider{err: errors.New("internal failure")}
	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	_, err := client.ReadJournalTimeRange(ctx, "TestDomain", "Log", base, base.Add(time.Hour))
	if err == nil {
		t.Fatal("expected error for generic provider error")
	}
}

// errorJournalProvider always returns the configured error from read methods.
type errorJournalProvider struct {
	err error
}

func (p *errorJournalProvider) ListJournals(_ context.Context, _ string) ([]string, error) {
	return []string{"Log"}, nil
}

func (p *errorJournalProvider) ReadTimeRange(_ context.Context, _, _ string,
	_, _ time.Time, _ int) (*JournalResult, error) {
	return nil, p.err
}

func (p *errorJournalProvider) ReadStartAfter(_ context.Context, _, _ string,
	_ []byte, _ time.Time, _ int) (*JournalResult, error) {
	return nil, p.err
}

func TestJournalGetNameListContinueAfterInvalid(t *testing.T) {
	jp := newMemJournalProvider()
	jp.addJournal("TestDomain", "AlarmLog")

	ctx, cancel, client := journalTestSetup(t, jp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass:   ObjectClassJournal,
		Scope:         ObjectScopeDomain,
		DomainID:      "TestDomain",
		ContinueAfter: "NonExistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid continueAfter")
	}
}
