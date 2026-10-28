//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// In-memory file store backed by the fixture "files" section
// ---------------------------------------------------------------------------

type memFile struct {
	data    []byte
	modTime time.Time
}

type memHandle struct {
	data []byte
	pos  int
}

// fixtureFileStore implements mms.FileProvider over the fixture files.
//
// Listings follow the per-level convention of some devices, so the
// libiec61850 client sees the form its own server does not produce (that
// server flattens the store into full '/' paths): an empty FileSpec lists
// the top-level files plus one zero-size entry "<dir>\" without
// lastModified per subdirectory; a directory name, with or without a
// trailing separator, lists the files below it by full path; a file name
// lists that file.
type fixtureFileStore struct {
	mu    sync.Mutex
	files map[string]*memFile
}

func newFixtureFileStore(fix *fixtureFile) (*fixtureFileStore, error) {
	st := &fixtureFileStore{files: make(map[string]*memFile)}
	for name, f := range fix.Files {
		data, err := f.bytes()
		if err != nil {
			return nil, fmt.Errorf("file %q: %w", name, err)
		}
		st.files[name] = &memFile{data: data, modTime: f.modTime()}
	}
	return st, nil
}

func (s *fixtureFileStore) List(_ context.Context, req mms.FileListRequest) (*mms.FileListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	spec := strings.TrimRight(req.FileSpec, `/\`)
	var entries []mms.FileEntry
	dirs := map[string]bool{}
	for name, f := range s.files {
		switch {
		case spec == "":
			if dir, _, nested := strings.Cut(name, "/"); nested {
				if !dirs[dir] {
					dirs[dir] = true
					entries = append(entries, mms.FileEntry{Name: dir + `\`})
				}
				continue
			}
		case name == spec, strings.HasPrefix(name, spec+"/"):
		default:
			continue
		}
		entries = append(entries, mms.FileEntry{Name: name, Size: int64(len(f.data)), LastModified: f.modTime})
	}
	if spec != "" && len(entries) == 0 {
		return nil, fs.ErrNotExist
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name < entries[b].Name })

	res := &mms.FileListResult{}
	for _, e := range entries {
		if req.ContinueAfter == "" || e.Name > req.ContinueAfter {
			res.Entries = append(res.Entries, e)
		}
	}
	return res, nil
}

func (s *fixtureFileStore) Open(_ context.Context, path string) (mms.FileHandle, mms.FileAttributes, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return nil, mms.FileAttributes{}, fs.ErrNotExist
	}
	return &memHandle{data: f.data}, mms.FileAttributes{Size: int64(len(f.data)), LastModified: f.modTime}, nil
}

func (s *fixtureFileStore) Read(_ context.Context, handle mms.FileHandle, maxBytes int) ([]byte, bool, error) {
	h := handle.(*memHandle)
	end := h.pos + maxBytes
	if end > len(h.data) {
		end = len(h.data)
	}
	chunk := h.data[h.pos:end]
	h.pos = end
	return chunk, h.pos < len(h.data), nil
}

func (s *fixtureFileStore) Close(context.Context, mms.FileHandle) error { return nil }

func (s *fixtureFileStore) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.files[path]; !ok {
		return fs.ErrNotExist
	}
	delete(s.files, path)
	return nil
}

func (s *fixtureFileStore) Rename(_ context.Context, currentName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[currentName]
	if !ok {
		return fs.ErrNotExist
	}
	if _, exists := s.files[newName]; exists {
		return mms.ErrFileAccessDenied
	}
	delete(s.files, currentName)
	s.files[newName] = f
	return nil
}

// ObtainFile copies within the store. A real server would fetch
// sourceFile from the client; the fixture store treats the source as one
// of its own files so the service can be exercised end to end.
func (s *fixtureFileStore) ObtainFile(_ context.Context, sourceFile, destinationFile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[sourceFile]
	if !ok {
		return fs.ErrNotExist
	}
	s.files[destinationFile] = &memFile{data: append([]byte(nil), f.data...), modTime: time.Now().UTC().Truncate(time.Second)}
	return nil
}

func (s *fixtureFileStore) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for n := range s.files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Journals backed by the fixture "journals" sections
// ---------------------------------------------------------------------------

// fixtureJournals implements mms.JournalProvider. Entries carry the data
// variable plus the 7-bit ReasonCode bit-string, matching what
// libiec61850 servers emit.
type fixtureJournals struct {
	journals map[string]map[string][]mms.JournalEntry // domain → journal → entries
}

func newFixtureJournals(fix *fixtureFile) (*fixtureJournals, error) {
	j := &fixtureJournals{journals: make(map[string]map[string][]mms.JournalEntry)}
	for domain, d := range fix.Domains {
		for name, journal := range d.Journals {
			var entries []mms.JournalEntry
			for _, e := range journal.Entries {
				tm, err := time.Parse(time.RFC3339Nano, e.OccurrenceTime)
				if err != nil {
					return nil, fmt.Errorf("journal %s/%s entry %d: %w", domain, name, e.EntryID, err)
				}
				val, err := makeScalarValue(e.Type, e.Value)
				if err != nil {
					return nil, fmt.Errorf("journal %s/%s entry %d: %w", domain, name, e.EntryID, err)
				}
				entries = append(entries, mms.JournalEntry{
					EntryID:        entryIDBytes(e.EntryID),
					OccurrenceTime: tm,
					Variables: []mms.JournalVariable{
						{Tag: e.Tag, Value: val},
						{Tag: "ReasonCode", Value: reasonCodeValue(e.ReasonCode)},
					},
				})
			}
			sort.SliceStable(entries, func(a, b int) bool { return entries[a].OccurrenceTime.Before(entries[b].OccurrenceTime) })
			if j.journals[domain] == nil {
				j.journals[domain] = make(map[string][]mms.JournalEntry)
			}
			j.journals[domain][name] = entries
		}
	}
	return j, nil
}

// reasonCodeValue encodes a reason code as libiec61850 does: a 7-bit
// bit-string where bit i (MSB first) carries 1<<i.
func reasonCodeValue(code int) *mms.Value {
	var b byte
	for i := 0; i < 7; i++ {
		if code&(1<<i) != 0 {
			b |= 0x80 >> i
		}
	}
	return mms.NewBitStringWithLength([]byte{b}, 7)
}

func (j *fixtureJournals) entries(domain, journal string) ([]mms.JournalEntry, error) {
	d, ok := j.journals[domain]
	if !ok {
		return nil, fs.ErrNotExist
	}
	e, ok := d[journal]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return e, nil
}

func (j *fixtureJournals) ListJournals(_ context.Context, domain string) ([]string, error) {
	var names []string
	for n := range j.journals[domain] {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (j *fixtureJournals) ReadTimeRange(_ context.Context, domain, journal string, start, stop time.Time, maxEntries int) (*mms.JournalResult, error) {
	all, err := j.entries(domain, journal)
	if err != nil {
		return nil, err
	}
	res := &mms.JournalResult{}
	for _, e := range all {
		if e.OccurrenceTime.Before(start) || e.OccurrenceTime.After(stop) {
			continue
		}
		if len(res.Entries) == maxEntries {
			res.MoreFollows = true
			break
		}
		res.Entries = append(res.Entries, e)
	}
	return res, nil
}

func (j *fixtureJournals) ReadStartAfter(_ context.Context, domain, journal string, afterID []byte, afterTime time.Time, maxEntries int) (*mms.JournalResult, error) {
	all, err := j.entries(domain, journal)
	if err != nil {
		return nil, err
	}
	res := &mms.JournalResult{}
	found := false
	for _, e := range all {
		if !found {
			found = e.OccurrenceTime.Equal(afterTime) && string(e.EntryID) == string(afterID)
			continue
		}
		if len(res.Entries) == maxEntries {
			res.MoreFollows = true
			break
		}
		res.Entries = append(res.Entries, e)
	}
	if !found {
		return nil, fs.ErrNotExist
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// InformationReports triggered by writes to interop/report-trigger
// ---------------------------------------------------------------------------

// reportTriggerAbort written to interop/report-trigger makes the go-mms
// server abort the writing association instead of sending reports.
const reportTriggerAbort = -1

// onReportTrigger implements the interop/report-trigger contract: -1 aborts
// the association (ServerConn.Abort); any other value sends every fixture
// InformationReport — listOfVariable reports on the writing connection,
// named-list reports via Server.Broadcast — followed by an UnsolicitedStatus.
func onReportTrigger(ctx context.Context, srv *mms.Server, fix *fixtureFile, val *mms.Value, read func(ref string) (*mms.Value, error)) error {
	sc := mms.ServerConnFromContext(ctx)
	if sc == nil {
		return fmt.Errorf("no server connection in context")
	}
	if n, _ := val.Int64(); n == reportTriggerAbort {
		return sc.Abort(ctx)
	}
	for _, r := range fix.InformationReports {
		req := &mms.InformationReportRequest{}
		for _, ref := range r.Variables {
			v, err := read(ref)
			if err != nil {
				return err
			}
			req.Values = append(req.Values, v)
			if r.VMDList == "" {
				dom, item, _ := strings.Cut(ref, "/")
				req.Variables = append(req.Variables, mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(dom), ItemID: mms.ItemID(item)})
			}
		}
		var err error
		if r.VMDList != "" {
			req.ListName = &mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: mms.ItemID(r.VMDList)}
			err = srv.Broadcast(ctx, req)
		} else {
			err = sc.SendInformationReport(ctx, req)
		}
		if err != nil {
			return err
		}
	}
	return sc.SendUnsolicitedStatus(ctx, mms.ServerStatus{
		Logical:  mms.VMDLogicalStatus(fix.Status.Logical),
		Physical: mms.VMDPhysicalStatus(fix.Status.Physical),
	})
}
