package mms

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-mms/internal/pdu"
)

// FileOpenResult holds the result of a [Client.FileOpen] call.
type FileOpenResult struct {
	FrsmID       int32
	Size         int64
	LastModified time.Time
}

// FileReadResult holds one chunk from a [Client.FileRead] call.
type FileReadResult struct {
	Data        []byte
	MoreFollows bool
}

// FileDirectoryEntry describes one file entry returned by [Client.FileDirectory].
type FileDirectoryEntry struct {
	Name         string
	Size         int64
	LastModified time.Time
}

// FileOpen opens a file on the remote MMS server and returns a file
// handle (frsmId) that can be used with [Client.FileRead] and
// [Client.FileClose]. The optional [FileOpenOptions] configures the
// initial read position; the zero value starts at position 0.
func (c *Client) FileOpen(ctx context.Context, fileName string, opts ...FileOpenOptions) (*FileOpenResult, error) {
	var opt FileOpenOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalFileOpenRequest(invokeID, fileName, opt.InitialPosition)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal file-open request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceFileOpen {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected FileOpen response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalFileOpenResponse(confirmed.ServiceData.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	return &FileOpenResult{
		FrsmID:       result.FrsmID,
		Size:         result.Size,
		LastModified: result.LastModified,
	}, nil
}

// FileRead reads a chunk of data from a previously opened file handle.
// Repeat until [FileReadResult.MoreFollows] is false.
func (c *Client) FileRead(ctx context.Context, frsmID int32) (*FileReadResult, error) {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalFileReadRequest(invokeID, frsmID)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal file-read request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceFileRead {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected FileRead response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalFileReadResponse(confirmed.ServiceData.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	return &FileReadResult{
		Data:        result.Data,
		MoreFollows: result.MoreFollows,
	}, nil
}

// FileClose closes a previously opened file handle on the remote server.
func (c *Client) FileClose(ctx context.Context, frsmID int32) error {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalFileCloseRequest(invokeID, frsmID)
	if err != nil {
		return fmt.Errorf("mms: marshal file-close request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return err
	}

	if confirmed.ServiceKind != pdu.ServiceFileClose {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected FileClose response, got %s", confirmed.ServiceKind),
		}
	}

	return nil
}

// FileDelete deletes a file on the remote MMS server.
func (c *Client) FileDelete(ctx context.Context, fileName string) error {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalFileDeleteRequest(invokeID, fileName)
	if err != nil {
		return fmt.Errorf("mms: marshal file-delete request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return err
	}

	if confirmed.ServiceKind != pdu.ServiceFileDelete {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected FileDelete response, got %s", confirmed.ServiceKind),
		}
	}

	return nil
}

// FileRename renames a file on the remote MMS server.
func (c *Client) FileRename(ctx context.Context, currentName, newName string) error {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalFileRenameRequest(invokeID, currentName, newName)
	if err != nil {
		return fmt.Errorf("mms: marshal file-rename request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return err
	}

	if confirmed.ServiceKind != pdu.ServiceFileRename {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected FileRename response, got %s", confirmed.ServiceKind),
		}
	}

	return nil
}

// ObtainFile requests the remote MMS server to copy a file from
// sourceFile to destinationFile. This is a two-party file transfer
// where the server fetches the source file.
func (c *Client) ObtainFile(ctx context.Context, sourceFile, destinationFile string) error {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalObtainFileRequest(invokeID, sourceFile, destinationFile)
	if err != nil {
		return fmt.Errorf("mms: marshal obtain-file request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return err
	}

	if confirmed.ServiceKind != pdu.ServiceObtainFile {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected ObtainFile response, got %s", confirmed.ServiceKind),
		}
	}

	return nil
}

// FileDirectory lists files on the remote MMS server with pagination support.
// Use [FileDirectoryRequest.ContinueAfter] to page through large directories.
func (c *Client) FileDirectory(ctx context.Context, req FileDirectoryRequest) (*FileDirectoryResult, error) {
	invokeID := c.nextInvokeID()
	reqBytes, err := pdu.MarshalFileDirectoryRequest(invokeID, req.FileSpec, req.ContinueAfter)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal file-directory request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceFileDirectory {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected FileDirectory response, got %s", confirmed.ServiceKind),
		}
	}

	wireEntries, moreFollows, err := pdu.UnmarshalFileDirectoryResponse(confirmed.ServiceData.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	entries := make([]FileDirectoryEntry, len(wireEntries))
	for i, we := range wireEntries {
		entries[i] = FileDirectoryEntry{
			Name:         we.FileName,
			Size:         we.Size,
			LastModified: we.LastModified,
		}
	}
	var continueAfter string
	if len(entries) > 0 {
		continueAfter = entries[len(entries)-1].Name
	}
	return &FileDirectoryResult{
		Entries:       entries,
		MoreFollows:   moreFollows,
		ContinueAfter: continueAfter,
	}, nil
}

// FileReadAll reads all remaining data from a previously opened file handle.
// It repeatedly calls [Client.FileRead] until MoreFollows is false and
// returns the concatenated data.
func (c *Client) FileReadAll(ctx context.Context, frsmID int32) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := c.FileRead(ctx, frsmID)
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk.Data...)
		if !chunk.MoreFollows {
			return buf, nil
		}
	}
}

// DownloadFile is a convenience method that opens a file, reads all its
// content, and closes the handle. It returns the file data and metadata.
func (c *Client) DownloadFile(ctx context.Context, fileName string) ([]byte, *FileOpenResult, error) {
	openResult, err := c.FileOpen(ctx, fileName)
	if err != nil {
		return nil, nil, fmt.Errorf("mms: download %q: open: %w", fileName, err)
	}

	data, err := c.FileReadAll(ctx, openResult.FrsmID)
	if err != nil {
		_ = c.FileClose(ctx, openResult.FrsmID)
		return nil, nil, fmt.Errorf("mms: download %q: read: %w", fileName, err)
	}

	if err := c.FileClose(ctx, openResult.FrsmID); err != nil {
		return data, openResult, fmt.Errorf("mms: download %q: close: %w", fileName, err)
	}

	return data, openResult, nil
}

// FileDirectoryAll retrieves all file directory entries by automatically
// handling continuation. It repeatedly calls [Client.FileDirectory]
// until MoreFollows is false.
func (c *Client) FileDirectoryAll(ctx context.Context, fileSpec string) ([]FileDirectoryEntry, error) {
	var all []FileDirectoryEntry
	req := FileDirectoryRequest{FileSpec: fileSpec}
	prevToken := ""
	for {
		result, err := c.FileDirectory(ctx, req)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Entries...)
		if !result.MoreFollows || len(result.Entries) == 0 {
			return all, nil
		}
		if result.ContinueAfter == prevToken {
			return nil, &ProtocolError{
				Phase:   "mms",
				Message: fmt.Sprintf("file directory: pagination stalled (continuation token %q did not advance)", result.ContinueAfter),
			}
		}
		prevToken = result.ContinueAfter
		req.ContinueAfter = result.ContinueAfter
	}
}
