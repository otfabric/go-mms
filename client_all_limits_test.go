// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// namePage is one GetNameList-Response of a scripted server.
type namePage struct {
	names       []string
	moreFollows bool
}

// serveNamePages answers one GetNameList request with each page in turn
// and returns the client and a function that waits until every page was
// sent.
func serveNamePages(t *testing.T, pages []namePage) (*Client, func()) {
	t.Helper()
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	go srv.handleAssociation(context.Background())
	c, err := NewClient(context.Background(), mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Abort(context.Background()) })
	var wg sync.WaitGroup
	wg.Go(func() {
		for _, p := range pages {
			invokeID, _, _ := srv.handleDataRequest(context.Background())
			if invokeID == 0 {
				return
			}
			srv.sendGetNameListResponse(context.Background(), invokeID, p.names, p.moreFollows)
		}
	})
	return c, wg.Wait
}

var allVars = NameListRequest{ObjectClass: ObjectClassNamedVariable, Scope: ObjectScopeDomain, DomainID: "D"}

// A continuation point the server already sent ends GetNameListAll, also
// when it is not the previous one.
func TestGetNameListAllRepeatedToken(t *testing.T) {
	c, served := serveNamePages(t, []namePage{{[]string{"A"}, true}, {[]string{"B"}, true}, {[]string{"A"}, true}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	names, err := c.GetNameListAll(ctx, allVars)
	if _, ok := errors.AsType[*ProtocolError](err); !ok || !errors.Is(err, ErrProtocol) {
		t.Fatalf("GetNameListAll = %q, %v; want a ProtocolError after the third page", names, err)
	}
	served()
}

// pagesOfNames returns pages of 20,000 names (60,000 octets) with total
// names in all; each page ends with a name of its own.
func pagesOfNames(total int) []namePage {
	const perPage = 20_000
	var pages []namePage
	for i := 0; total > 0; i++ {
		n := min(perPage, total)
		total -= n
		names := slices.Repeat([]string{"x"}, n)
		names[n-1] = fmt.Sprintf("p%04d", i)
		pages = append(pages, namePage{names, total > 0})
	}
	return pages
}

// GetNameListAll accumulates at most maxListAll names.
func TestGetNameListAllCeiling(t *testing.T) {
	for _, tc := range []struct {
		total int
		ok    bool
	}{
		{maxListAll, true},
		{maxListAll + 1, false},
	} {
		c, served := serveNamePages(t, pagesOfNames(tc.total))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		names, err := c.GetNameListAll(ctx, allVars)
		cancel()
		served()
		if tc.ok {
			if err != nil || len(names) != tc.total {
				t.Errorf("%d names: got %d, %v", tc.total, len(names), err)
			}
			continue
		}
		if !errors.Is(err, ErrProtocol) {
			t.Errorf("%d names: got %d names, %v; want an error matching ErrProtocol", tc.total, len(names), err)
		}
	}
}

// The ceiling counts names over all pages: 0, 1, limit-1, limit, limit+1.
func TestGetNameListAllCeilingBoundaries(t *testing.T) {
	const limit = 4
	for _, tc := range []struct {
		pages []namePage
		ok    bool
	}{
		{[]namePage{{nil, false}}, true},
		{[]namePage{{[]string{"a"}, false}}, true},
		{[]namePage{{[]string{"a", "b"}, true}, {[]string{"c"}, false}}, true},
		{[]namePage{{[]string{"a", "b"}, true}, {[]string{"c", "d"}, false}}, true},
		{[]namePage{{[]string{"a", "b"}, true}, {[]string{"c", "d", "e"}, false}}, false},
		{[]namePage{{[]string{"a", "b", "c", "d", "e"}, false}}, false},
	} {
		c, served := serveNamePages(t, tc.pages)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		names, err := c.getNameListAll(ctx, allVars, limit)
		cancel()
		served()
		if tc.ok != (err == nil) {
			t.Errorf("pages %v: %q, %v", tc.pages, names, err)
		}
	}
}

// endlessFileProvider serves one file whose chunks never end: each Read
// returns chunk and moreFollows true, up to reads times, then the end.
type endlessFileProvider struct {
	*memFileProvider
	chunk []byte
	reads int

	mu sync.Mutex
	n  int
}

func (p *endlessFileProvider) Read(_ context.Context, _ FileHandle, _ int) ([]byte, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	return p.chunk, p.n < p.reads, nil
}

func (p *endlessFileProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func endlessFileClient(t *testing.T, chunk []byte, reads int) (*Client, *endlessFileProvider) {
	t.Helper()
	fp := &endlessFileProvider{memFileProvider: newMemFileProvider(), chunk: chunk, reads: reads}
	fp.addFile("f", []byte("1"))
	srv := NewServer(ServerOptions{FileProvider: fp})
	c, served := serveLoopback(t, srv, DialOptions{})
	t.Cleanup(func() { _ = c.Abort(context.Background()); _ = waitServe(t, served) })
	return c, fp
}

// A chunk without data that announces more ends DownloadFile and
// FileReadAll after that one chunk.
func TestFileReadAllEmptyChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, fp := endlessFileClient(t, nil, 100)
	if data, _, err := c.DownloadFile(ctx, "f"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("DownloadFile = %d octets, %v after %d reads; want an error matching ErrProtocol", len(data), err, fp.count())
	}
	if n := fp.count(); n != 1 {
		t.Fatalf("%d reads, want 1", n)
	}

	c, fp = endlessFileClient(t, nil, 100)
	open, err := c.FileOpen(ctx, "f")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FileReadAll(ctx, open.FrsmID); !errors.Is(err, ErrProtocol) || fp.count() != 1 {
		t.Fatalf("FileReadAll = %v after %d reads; want an error matching ErrProtocol after 1", err, fp.count())
	}
}

// The ceiling counts the octets of every chunk: limit-1, limit, limit+1.
func TestFileReadAllCeiling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	chunk := []byte("abcd")
	for _, tc := range []struct {
		limit int64
		ok    bool
	}{
		{0, false},
		{11, false}, // limit-1
		{12, true},  // limit: three chunks of four octets
		{13, true},
		{maxFileReadAll, true},
	} {
		c, _ := endlessFileClient(t, chunk, 3)
		open, err := c.FileOpen(ctx, "f")
		if err != nil {
			t.Fatal(err)
		}
		data, err := c.fileReadAll(ctx, open.FrsmID, tc.limit)
		if tc.ok {
			if err != nil || string(data) != strings.Repeat("abcd", 3) {
				t.Errorf("limit %d: %q, %v", tc.limit, data, err)
			}
			continue
		}
		if !errors.Is(err, ErrProtocol) {
			t.Errorf("limit %d: %q, %v; want an error matching ErrProtocol", tc.limit, data, err)
		}
	}
}

// pagedFileProvider lists the directory in the pages given, one per
// List call, then an empty last page.
type pagedFileProvider struct {
	*memFileProvider
	pages [][]string

	mu sync.Mutex
	n  int
}

func (p *pagedFileProvider) List(_ context.Context, _ FileListRequest) (*FileListResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n >= len(p.pages) {
		return &FileListResult{}, nil
	}
	page := p.pages[p.n]
	p.n++
	res := &FileListResult{MoreFollows: true}
	for _, name := range page {
		res.Entries = append(res.Entries, FileEntry{Name: name, Size: 1})
	}
	return res, nil
}

func (p *pagedFileProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func pagedFileClient(t *testing.T, pages [][]string) (*Client, *pagedFileProvider) {
	t.Helper()
	fp := &pagedFileProvider{memFileProvider: newMemFileProvider(), pages: pages}
	srv := NewServer(ServerOptions{FileProvider: fp})
	c, served := serveLoopback(t, srv, DialOptions{})
	t.Cleanup(func() { _ = c.Abort(context.Background()); _ = waitServe(t, served) })
	return c, fp
}

// A continuation point the server already sent ends FileDirectoryAll,
// also when it is not the previous one.
func TestFileDirectoryAllRepeatedToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var pages [][]string
	for range 5 {
		pages = append(pages, []string{"A"}, []string{"B"})
	}
	c, fp := pagedFileClient(t, pages)
	entries, err := c.FileDirectoryAll(ctx, "")
	if _, ok := errors.AsType[*ProtocolError](err); !ok {
		t.Fatalf("FileDirectoryAll = %d entries, %v after %d pages; want a ProtocolError after 3", len(entries), err, fp.count())
	}
	if n := fp.count(); n != 3 {
		t.Fatalf("%d pages listed, want 3", n)
	}
}

// FileDirectoryAll counts entries over all pages: limit-1, limit, limit+1.
func TestFileDirectoryAllCeiling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pages := [][]string{{"a", "b"}, {"c", "d"}}
	for _, tc := range []struct {
		limit int
		ok    bool
	}{
		{3, false},
		{4, true},
		{5, true},
	} {
		c, _ := pagedFileClient(t, pages)
		entries, err := c.fileDirectoryAll(ctx, "", tc.limit)
		if tc.ok != (err == nil) || (tc.ok && len(entries) != 4) {
			t.Errorf("limit %d: %d entries, %v", tc.limit, len(entries), err)
		}
	}
}
