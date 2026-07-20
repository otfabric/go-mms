// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/serverconn"
)

// frsm tracks state for one open file handle (File Read State Machine).
type frsm struct {
	id     int32
	handle FileHandle
	path   string
}

// frsmTable manages per-connection open file handles. The server
// allocates a table per ServerConn; the table is cleaned up when
// the connection closes.
type frsmTable struct {
	mu      sync.RWMutex
	entries map[int32]*frsm
	nextID  int32
}

func newFRSMTable() *frsmTable {
	return &frsmTable{entries: make(map[int32]*frsm)}
}

func (t *frsmTable) alloc(path string, handle FileHandle) int32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	id := t.nextID
	t.entries[id] = &frsm{id: id, handle: handle, path: path}
	return id
}

func (t *frsmTable) get(id int32) (*frsm, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	f, ok := t.entries[id]
	return f, ok
}

func (t *frsmTable) remove(id int32) (*frsm, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.entries[id]
	if ok {
		delete(t.entries, id)
	}
	return f, ok
}

func (t *frsmTable) closeAll(_ context.Context, provider FileProvider) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, f := range t.entries {
		_ = provider.Close(cleanupCtx, f.handle)
	}
	t.entries = nil
}

// serverConnCtxKey is used internally to propagate the per-connection
// *ServerConn through the request context. This is an internal
// implementation detail — file handlers need access to the FRSM table
// on the connection. Avoid spreading this pattern; if more per-connection
// services appear, consider passing *ServerConn explicitly.
type serverConnCtxKey struct{}

func serverConnFromCtx(ctx context.Context) *ServerConn {
	sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn)
	return sc
}

// ServerConnFromContext extracts the server-side connection from the
// request context. The connection is available in callbacks that receive
// a context from an active MMS service request (e.g. variable read/write
// handlers, write interceptors). Returns nil if no connection is present.
func ServerConnFromContext(ctx context.Context) *ServerConn {
	return serverConnFromCtx(ctx)
}

// --- File service handlers ---

func (s *Server) handleFileOpen(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	req, err := pdu.UnmarshalFileOpenRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	handle, attrs, err := s.fileProvider.Open(ctx, req.FileName)
	if err != nil {
		return 0, false, nil, fileError(err)
	}

	sc := serverConnFromCtx(ctx)
	if sc == nil {
		_ = s.fileProvider.Close(ctx, handle)
		return 0, false, nil, fmt.Errorf("no server connection in context")
	}
	frsmID := sc.frsmTable.alloc(req.FileName, handle)

	payload, err := pdu.MarshalFileOpenResponse(frsmID, attrs.Size, attrs.LastModified)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal file-open response: %w", err)
	}
	return asn1util.TagNumFileOpen, true, payload, nil
}

func (s *Server) handleFileRead(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	frsmID, err := pdu.UnmarshalFileReadRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	sc := serverConnFromCtx(ctx)
	if sc == nil {
		return 0, false, nil, fmt.Errorf("no server connection in context")
	}
	f, ok := sc.frsmTable.get(frsmID)
	if !ok {
		return 0, false, nil, &serverconn.ServiceError{ErrorClass: fileErrClassFile, ErrorCode: fileErrOther}
	}

	maxBytes := 60000
	data, moreFollows, err := s.fileProvider.Read(ctx, f.handle, maxBytes)
	if err != nil {
		return 0, false, nil, fileError(err)
	}

	payload, err := pdu.MarshalFileReadResponse(data, moreFollows)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal file-read response: %w", err)
	}
	return asn1util.TagNumFileRead, true, payload, nil
}

//nolint:unparam // no response payload; signature required by dispatch pattern
func (s *Server) handleFileClose(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	frsmID, err := pdu.UnmarshalFileCloseRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	sc := serverConnFromCtx(ctx)
	if sc == nil {
		return 0, false, nil, fmt.Errorf("no server connection in context")
	}
	f, ok := sc.frsmTable.remove(frsmID)
	if !ok {
		return 0, false, nil, &serverconn.ServiceError{ErrorClass: fileErrClassFile, ErrorCode: fileErrOther}
	}

	if err := s.fileProvider.Close(ctx, f.handle); err != nil {
		return 0, false, nil, fileError(err)
	}

	return asn1util.TagNumFileClose, false, nil, nil
}

//nolint:unparam // no response payload; signature required by dispatch pattern
func (s *Server) handleFileDelete(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	fileName, err := pdu.UnmarshalFileDeleteRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	if err := s.fileProvider.Delete(ctx, fileName); err != nil {
		return 0, false, nil, fileError(err)
	}

	return asn1util.TagNumFileDelete, false, nil, nil
}

//nolint:unparam // no response payload; signature required by dispatch pattern
func (s *Server) handleFileRename(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	req, err := pdu.UnmarshalFileRenameRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	if err := s.fileProvider.Rename(ctx, req.CurrentFileName, req.NewFileName); err != nil {
		return 0, false, nil, fileError(err)
	}

	return asn1util.TagNumFileRename, false, nil, nil
}

//nolint:unparam // no response payload; signature required by dispatch pattern
func (s *Server) handleObtainFile(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	req, err := pdu.UnmarshalObtainFileRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	if err := s.fileProvider.ObtainFile(ctx, req.SourceFile, req.DestinationFile); err != nil {
		return 0, false, nil, fileError(err)
	}

	return asn1util.TagNumObtainFile, false, nil, nil
}

func (s *Server) handleFileDirectory(ctx context.Context, body []byte) (int, bool, []byte, error) {
	if s.fileProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	req, err := pdu.UnmarshalFileDirectoryRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	listResult, err := s.fileProvider.List(ctx, FileListRequest{
		FileSpec:      req.FileSpec,
		ContinueAfter: req.ContinueAfter,
	})
	if err != nil {
		return 0, false, nil, fileError(err)
	}

	wireEntries := make([]pdu.FileDirectoryEntry, len(listResult.Entries))
	for i, e := range listResult.Entries {
		wireEntries[i] = pdu.FileDirectoryEntry{
			FileName:     e.Name,
			Size:         e.Size,
			LastModified: e.LastModified,
		}
	}

	payload, err := pdu.MarshalFileDirectoryResponse(wireEntries, listResult.MoreFollows)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal file-directory response: %w", err)
	}
	return asn1util.TagNumFileDirectory, true, payload, nil
}

const (
	fileErrClassFile        = 11 // ServiceError errorClass "file" (CHOICE tag 11)
	fileErrOther            = 0
	fileErrFileAccessDenied = 6
	fileErrFileNonExistent  = 7
)

// ErrFileAccessDenied may be returned by [FileProvider] implementations
// to signal a permission error. The server maps it to MMS error class
// "file", code "file-access-denied".
var ErrFileAccessDenied = errors.New("mms: file access denied")

func fileError(err error) error {
	code := fileErrOther
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = fileErrFileNonExistent
	case errors.Is(err, fs.ErrPermission), errors.Is(err, ErrFileAccessDenied):
		code = fileErrFileAccessDenied
	}
	return &serverconn.ServiceError{ErrorClass: fileErrClassFile, ErrorCode: code}
}

// --- Public file types ---

// FileHandle is an opaque handle returned by [FileProvider.Open].
// The handle is valid until [FileProvider.Close] is called.
type FileHandle any

// FileEntry describes one file in a directory listing.
type FileEntry struct {
	Name         string
	Size         int64
	LastModified time.Time
}

// FileProvider is the server-side abstraction for MMS file services.
// Implementations back the FileOpen/Read/Close/Delete/Directory
// protocol services. The server does not access the filesystem
// directly; all I/O goes through this interface.
//
// Provider errors are mapped to MMS service errors by the server:
//   - [fs.ErrNotExist] → file-non-existent
//   - [fs.ErrPermission] or [ErrFileAccessDenied] → file-access-denied
//   - all other errors → other
type FileProvider interface {
	// List returns entries matching the request's FileSpec, with
	// optional pagination via ContinueAfter and MaxEntries.
	List(ctx context.Context, req FileListRequest) (*FileListResult, error)

	Open(ctx context.Context, path string) (FileHandle, FileAttributes, error)

	// Read returns the next chunk of data from handle, up to maxBytes.
	// On the final (or only) chunk the implementation must return
	// moreFollows=false with a nil error. Returning io.EOF is treated
	// as a provider error; use moreFollows=false to signal end-of-file.
	Read(ctx context.Context, handle FileHandle, maxBytes int) (data []byte, moreFollows bool, err error)

	Close(ctx context.Context, handle FileHandle) error
	Delete(ctx context.Context, path string) error

	// Rename renames a file. Implementations should return
	// [fs.ErrNotExist] if the source does not exist.
	Rename(ctx context.Context, currentName, newName string) error

	// ObtainFile copies sourceFile to destinationFile on the server.
	// Implementations may return [fs.ErrNotExist] if the source does
	// not exist, or [ErrFileAccessDenied] for permission errors.
	// Returning nil indicates success.
	ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
}

// FileAttributes describes file metadata returned when opening a file.
type FileAttributes struct {
	Size         int64
	LastModified time.Time
}
