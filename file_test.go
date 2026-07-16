// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sort"
	"sync"
	"testing"
	"time"
)

// --- In-memory file provider for tests ---

type memFile struct {
	name         string
	data         []byte
	lastModified time.Time
}

type memFileHandle struct {
	file   *memFile
	offset int
}

type memFileProvider struct {
	mu    sync.Mutex
	files map[string]*memFile
}

func newMemFileProvider() *memFileProvider {
	return &memFileProvider{files: make(map[string]*memFile)}
}

func (p *memFileProvider) addFile(name string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files[name] = &memFile{
		name:         name,
		data:         append([]byte(nil), data...),
		lastModified: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
	}
}

func (p *memFileProvider) List(_ context.Context, req FileListRequest) (*FileListResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var entries []FileEntry
	for _, f := range p.files {
		entries = append(entries, FileEntry{
			Name:         f.name,
			Size:         int64(len(f.data)),
			LastModified: f.lastModified,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return &FileListResult{
		Entries:     entries,
		MoreFollows: false,
	}, nil
}

func (p *memFileProvider) Open(_ context.Context, path string) (FileHandle, FileAttributes, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, ok := p.files[path]
	if !ok {
		return nil, FileAttributes{}, fs.ErrNotExist
	}
	return &memFileHandle{file: f, offset: 0}, FileAttributes{
		Size:         int64(len(f.data)),
		LastModified: f.lastModified,
	}, nil
}

// Read returns the next chunk of data. On the final chunk it returns
// moreFollows=false with nil error. It never returns io.EOF as normal
// completion — see FileProvider contract.
func (p *memFileProvider) Read(_ context.Context, handle FileHandle, maxBytes int) ([]byte, bool, error) {
	h, ok := handle.(*memFileHandle)
	if !ok {
		return nil, false, ErrFileAccessDenied
	}
	remaining := len(h.file.data) - h.offset
	if remaining <= 0 {
		return nil, false, nil
	}
	n := remaining
	if n > maxBytes {
		n = maxBytes
	}
	data := h.file.data[h.offset : h.offset+n]
	h.offset += n
	moreFollows := h.offset < len(h.file.data)
	return data, moreFollows, nil
}

func (p *memFileProvider) Close(_ context.Context, handle FileHandle) error {
	return nil
}

func (p *memFileProvider) Delete(_ context.Context, path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.files[path]; !ok {
		return fs.ErrNotExist
	}
	delete(p.files, path)
	return nil
}

func (p *memFileProvider) Rename(_ context.Context, currentName, newName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.files[currentName]
	if !ok {
		return fs.ErrNotExist
	}
	f.name = newName
	p.files[newName] = f
	delete(p.files, currentName)
	return nil
}

func (p *memFileProvider) ObtainFile(_ context.Context, sourceFile, destFile string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	src, ok := p.files[sourceFile]
	if !ok {
		return fs.ErrNotExist
	}
	p.files[destFile] = &memFile{
		name:         destFile,
		data:         append([]byte(nil), src.data...),
		lastModified: src.lastModified,
	}
	return nil
}

// --- Integration tests ---

func fileTestSetup(t *testing.T, fp FileProvider) (context.Context, context.CancelFunc, *Client) {
	t.Helper()
	srv := NewServer(ServerOptions{FileProvider: fp})
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

func TestFileDirectoryEndToEnd(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("alpha.dat", []byte("aaa"))
	fp.addFile("beta.dat", []byte("bbb"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.FileDirectory(ctx, FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	if result.Entries[0].Name != "alpha.dat" {
		t.Errorf("entry[0].Name = %q, want alpha.dat", result.Entries[0].Name)
	}
	if result.Entries[1].Name != "beta.dat" {
		t.Errorf("entry[1].Name = %q, want beta.dat", result.Entries[1].Name)
	}
	if result.Entries[0].Size != 3 {
		t.Errorf("entry[0].Size = %d, want 3", result.Entries[0].Size)
	}
}

func TestFileOpenReadCloseEndToEnd(t *testing.T) {
	content := []byte("Hello, MMS file services!")
	fp := newMemFileProvider()
	fp.addFile("test.txt", content)

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	openResult, err := client.FileOpen(ctx, "test.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}
	if openResult.Size != int64(len(content)) {
		t.Errorf("FileOpen Size = %d, want %d", openResult.Size, len(content))
	}

	var allData []byte
	for {
		readResult, err := client.FileRead(ctx, openResult.FrsmID)
		if err != nil {
			t.Fatalf("FileRead: %v", err)
		}
		allData = append(allData, readResult.Data...)
		if !readResult.MoreFollows {
			break
		}
	}

	if !bytes.Equal(allData, content) {
		t.Errorf("read data = %q, want %q", allData, content)
	}

	if err := client.FileClose(ctx, openResult.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}
}

func TestFileDeleteEndToEnd(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("delete-me.txt", []byte("data"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	if err := client.FileDelete(ctx, "delete-me.txt"); err != nil {
		t.Fatalf("FileDelete: %v", err)
	}

	dirResult, err := client.FileDirectory(ctx, FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory after delete: %v", err)
	}
	if len(dirResult.Entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(dirResult.Entries))
	}
}

func TestFileServicesNotConfigured(t *testing.T) {
	ctx, cancel, client := fileTestSetup(t, nil)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, err := client.FileDirectory(ctx, FileDirectoryRequest{})
	if err == nil {
		t.Fatal("expected error for file-directory without provider")
	}
}

// --- Negative / invariant tests ---

func TestFileOpenNonExistent(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, err := client.FileOpen(ctx, "does-not-exist.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Class != ErrorClassFile {
		t.Errorf("error class = %s, want File", svcErr.Class)
	}
	if svcErr.Code != 7 { // file-non-existent
		t.Errorf("error code = %d, want 7", svcErr.Code)
	}
}

func TestFileReadInvalidFrsm(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, err := client.FileRead(ctx, 9999)
	if err == nil {
		t.Fatal("expected error for invalid frsmID")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
}

func TestFileCloseInvalidFrsm(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	err := client.FileClose(ctx, 9999)
	if err == nil {
		t.Fatal("expected error for invalid frsmID")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
}

func TestFileDeleteNonExistent(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	err := client.FileDelete(ctx, "ghost.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Class != ErrorClassFile {
		t.Errorf("error class = %s, want File", svcErr.Class)
	}
	if svcErr.Code != 7 { // file-non-existent
		t.Errorf("error code = %d, want 7", svcErr.Code)
	}
}

func TestFileDoubleClose(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("dbl.txt", []byte("data"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	res, err := client.FileOpen(ctx, "dbl.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	if err := client.FileClose(ctx, res.FrsmID); err != nil {
		t.Fatalf("first FileClose: %v", err)
	}

	err = client.FileClose(ctx, res.FrsmID)
	if err == nil {
		t.Fatal("expected error on second close")
	}
}

func TestFileRenameEndToEnd(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("old.txt", []byte("rename-me"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()

	err := client.FileRename(ctx, "old.txt", "new.txt")
	if err != nil {
		t.Fatalf("FileRename: %v", err)
	}

	dirResult, err := client.FileDirectory(ctx, FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if len(dirResult.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(dirResult.Entries))
	}
	if dirResult.Entries[0].Name != "new.txt" {
		t.Errorf("expected 'new.txt', got %q", dirResult.Entries[0].Name)
	}

	_ = client.Close(ctx)
}

func TestFileRenameNotFound(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()

	err := client.FileRename(ctx, "nonexistent.txt", "new.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	t.Logf("expected error: %v", err)

	_ = client.Close(ctx)
}

func TestObtainFileEndToEnd(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("source.txt", []byte("copy-me"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()

	err := client.ObtainFile(ctx, "source.txt", "dest.txt")
	if err != nil {
		t.Fatalf("ObtainFile: %v", err)
	}

	dirResult, err := client.FileDirectory(ctx, FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if len(dirResult.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(dirResult.Entries))
	}

	result, err := client.FileOpen(ctx, "dest.txt")
	if err != nil {
		t.Fatalf("FileOpen dest.txt: %v", err)
	}

	readResult, err := client.FileRead(ctx, result.FrsmID)
	if err != nil {
		t.Fatalf("FileRead: %v", err)
	}
	if string(readResult.Data) != "copy-me" {
		t.Errorf("expected 'copy-me', got %q", string(readResult.Data))
	}

	_ = client.FileClose(ctx, result.FrsmID)
	_ = client.Close(ctx)
}

func TestFileReadAfterClose(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("rac.txt", []byte("data"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	res, err := client.FileOpen(ctx, "rac.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	if err := client.FileClose(ctx, res.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}

	_, err = client.FileRead(ctx, res.FrsmID)
	if err == nil {
		t.Fatal("expected error on read after close")
	}
}

func TestFileDisconnectClosesHandles(t *testing.T) {
	fp := newClosingMemFileProvider()
	fp.addFile("handle.txt", []byte("data"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()

	_, err := client.FileOpen(ctx, "handle.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	_ = client.Close(ctx)

	time.Sleep(50 * time.Millisecond)

	fp.mu.Lock()
	count := fp.closeCount
	fp.mu.Unlock()
	if count == 0 {
		t.Error("expected provider Close to be called on disconnect")
	}
}

// closingMemFileProvider tracks calls to Close.
type closingMemFileProvider struct {
	memFileProvider
	closeCount int
}

func newClosingMemFileProvider() *closingMemFileProvider {
	return &closingMemFileProvider{
		memFileProvider: memFileProvider{files: make(map[string]*memFile)},
	}
}

func (p *closingMemFileProvider) Close(_ context.Context, _ FileHandle) error {
	p.mu.Lock()
	p.closeCount++
	p.mu.Unlock()
	return nil
}

func TestFileRenameCollision(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("a.txt", []byte("aaa"))
	fp.addFile("b.txt", []byte("bbb"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	err := client.FileRename(ctx, "a.txt", "b.txt")
	if err != nil {
		t.Fatalf("rename collision: expected overwrite, got error: %v", err)
	}

	dirResult, err := client.FileDirectory(ctx, FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if len(dirResult.Entries) != 1 {
		t.Fatalf("expected 1 entry after overwrite rename, got %d", len(dirResult.Entries))
	}
	if dirResult.Entries[0].Name != "b.txt" {
		t.Errorf("expected remaining file 'b.txt', got %q", dirResult.Entries[0].Name)
	}

	result, err := client.FileOpen(ctx, "b.txt")
	if err != nil {
		t.Fatalf("FileOpen b.txt: %v", err)
	}
	readResult, err := client.FileRead(ctx, result.FrsmID)
	if err != nil {
		t.Fatalf("FileRead: %v", err)
	}
	if string(readResult.Data) != "aaa" {
		t.Errorf("b.txt content = %q, want 'aaa' (a.txt's original content)", readResult.Data)
	}
	_ = client.FileClose(ctx, result.FrsmID)
}

func TestObtainFileDestExists(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("src.txt", []byte("source"))
	fp.addFile("dst.txt", []byte("old"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	err := client.ObtainFile(ctx, "src.txt", "dst.txt")
	if err != nil {
		t.Fatalf("ObtainFile: %v", err)
	}

	result, err := client.FileOpen(ctx, "dst.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}
	readResult, err := client.FileRead(ctx, result.FrsmID)
	if err != nil {
		t.Fatalf("FileRead: %v", err)
	}
	if string(readResult.Data) != "source" {
		t.Errorf("expected overwritten content 'source', got %q", readResult.Data)
	}
	_ = client.FileClose(ctx, result.FrsmID)
}

func TestFileMultiChunkRead(t *testing.T) {
	data := bytes.Repeat([]byte("X"), 150000)

	fp := newMemFileProvider()
	fp.addFile("big.bin", data)

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.FileOpen(ctx, "big.bin")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	var allData []byte
	readCount := 0
	sawMoreFollows := false
	for {
		readResult, err := client.FileRead(ctx, result.FrsmID)
		if err != nil {
			t.Fatalf("FileRead: %v", err)
		}
		readCount++
		allData = append(allData, readResult.Data...)
		if readResult.MoreFollows {
			sawMoreFollows = true
		}
		if !readResult.MoreFollows {
			break
		}
	}

	if !bytes.Equal(allData, data) {
		t.Errorf("read %d bytes, want %d bytes", len(allData), len(data))
	}
	if readCount < 2 {
		t.Errorf("expected multiple FileRead calls, got %d", readCount)
	}
	if !sawMoreFollows {
		t.Error("expected at least one intermediate response with MoreFollows=true")
	}

	_ = client.FileClose(ctx, result.FrsmID)
}

func TestFileNameWithPathSeparators(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("subdir/nested/file.txt", []byte("deep"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.FileOpen(ctx, "subdir/nested/file.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	readResult, err := client.FileRead(ctx, result.FrsmID)
	if err != nil {
		t.Fatalf("FileRead: %v", err)
	}
	if string(readResult.Data) != "deep" {
		t.Errorf("got %q, want 'deep'", readResult.Data)
	}
	_ = client.FileClose(ctx, result.FrsmID)
}

// testIdentifyHandler is a minimal handler for tests that need basic
// server functionality for establishing associations.
func testIdentifyHandler(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
	return &ServerIdentity{Vendor: "Test", Model: "FileTest", Revision: "1.0"}, nil
}

func testStatusHandler(_ context.Context, _ StatusRequest) (*ServerStatus, error) {
	return &ServerStatus{
		Logical:  VMDLogicalStatusStateChangesAllowed,
		Physical: VMDPhysicalStatusOperational,
	}, nil
}

// --- Convenience method integration tests ---

func TestFileReadAll(t *testing.T) {
	content := []byte("Hello, FileReadAll!")
	fp := newMemFileProvider()
	fp.addFile("readall.txt", content)

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	openResult, err := client.FileOpen(ctx, "readall.txt")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	data, err := client.FileReadAll(ctx, openResult.FrsmID)
	if err != nil {
		t.Fatalf("FileReadAll: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("FileReadAll data = %q, want %q", data, content)
	}

	if err := client.FileClose(ctx, openResult.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}
}

func TestFileReadAllMultiChunk(t *testing.T) {
	content := bytes.Repeat([]byte("Y"), 150000)
	fp := newMemFileProvider()
	fp.addFile("big-readall.bin", content)

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	openResult, err := client.FileOpen(ctx, "big-readall.bin")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}

	data, err := client.FileReadAll(ctx, openResult.FrsmID)
	if err != nil {
		t.Fatalf("FileReadAll: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("FileReadAll data length = %d, want %d", len(data), len(content))
	}

	if err := client.FileClose(ctx, openResult.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}
}

func TestDownloadFile(t *testing.T) {
	content := []byte("download-me-content")
	fp := newMemFileProvider()
	fp.addFile("test.dat", content)

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	data, openResult, err := client.DownloadFile(ctx, "test.dat")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if openResult == nil {
		t.Fatal("DownloadFile returned nil FileOpenResult")
	}
	if openResult.Size != int64(len(content)) {
		t.Errorf("FileOpenResult.Size = %d, want %d", openResult.Size, len(content))
	}
	if !bytes.Equal(data, content) {
		t.Errorf("DownloadFile data = %q, want %q", data, content)
	}
}

func TestDownloadFileNonExistent(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, _, err := client.DownloadFile(ctx, "no-such-file.bin")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestFileDirectoryAll(t *testing.T) {
	fp := newMemFileProvider()
	fp.addFile("a.txt", []byte("aaa"))
	fp.addFile("b.txt", []byte("bbb"))
	fp.addFile("c.txt", []byte("ccc"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	entries, err := client.FileDirectoryAll(ctx, "")
	if err != nil {
		t.Fatalf("FileDirectoryAll: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("FileDirectoryAll entries = %d, want 3", len(entries))
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	sort.Strings(names)
	want := []string{"a.txt", "b.txt", "c.txt"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("entry[%d] = %q, want %q", i, n, want[i])
		}
	}
}

// paginatingFileProvider wraps memFileProvider to return entries one at
// a time with MoreFollows=true, forcing FileDirectoryAll to paginate.
type paginatingFileProvider struct {
	memFileProvider
	pageSize int
}

func newPaginatingFileProvider(pageSize int) *paginatingFileProvider {
	return &paginatingFileProvider{
		memFileProvider: memFileProvider{files: make(map[string]*memFile)},
		pageSize:        pageSize,
	}
}

func (p *paginatingFileProvider) List(_ context.Context, req FileListRequest) (*FileListResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var all []FileEntry
	for _, f := range p.files {
		all = append(all, FileEntry{
			Name:         f.name,
			Size:         int64(len(f.data)),
			LastModified: f.lastModified,
		})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	start := 0
	if req.ContinueAfter != "" {
		for i, e := range all {
			if e.Name == req.ContinueAfter {
				start = i + 1
				break
			}
		}
	}

	remaining := all[start:]
	pageSize := p.pageSize
	if pageSize <= 0 {
		pageSize = 1
	}
	if len(remaining) <= pageSize {
		return &FileListResult{Entries: remaining, MoreFollows: false}, nil
	}
	return &FileListResult{Entries: remaining[:pageSize], MoreFollows: true}, nil
}

func TestFileDirectoryAllPaginated(t *testing.T) {
	fp := newPaginatingFileProvider(1)
	fp.addFile("alpha.bin", []byte("1"))
	fp.addFile("bravo.bin", []byte("2"))
	fp.addFile("charlie.bin", []byte("3"))

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	entries, err := client.FileDirectoryAll(ctx, "")
	if err != nil {
		t.Fatalf("FileDirectoryAll: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("FileDirectoryAll entries = %d, want 3", len(entries))
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	sort.Strings(names)
	want := []string{"alpha.bin", "bravo.bin", "charlie.bin"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("entry[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestFileDirectoryAllEmpty(t *testing.T) {
	fp := newMemFileProvider()

	ctx, cancel, client := fileTestSetup(t, fp)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	entries, err := client.FileDirectoryAll(ctx, "")
	if err != nil {
		t.Fatalf("FileDirectoryAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("FileDirectoryAll entries = %d, want 0", len(entries))
	}
}
