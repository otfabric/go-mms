// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// openHandles counts the handles of a raisingFiles that are open.
type openHandles struct {
	*raisingFiles
	open atomic.Int32
}

func (p *openHandles) Open(ctx context.Context, path string) (FileHandle, FileAttributes, error) {
	h, attrs, err := p.raisingFiles.Open(ctx, path)
	if err == nil {
		p.open.Add(1)
	}
	return h, attrs, err
}

func (p *openHandles) Close(ctx context.Context, h FileHandle) error {
	p.open.Add(-1)
	return p.raisingFiles.Close(ctx, h)
}

// A FileOpen with an initial position keeps the handle it opened only when
// the skip to that position succeeds. When the skip fails, or a
// FileProvider.Read does not return normally, the handle is closed and the
// request is answered with an error.
func TestFileOpenPositionClosesHandle(t *testing.T) {
	positionInvalid := func(err error) bool {
		se, ok := errors.AsType[*ServiceError](err)
		return ok && int(se.Class) == fileErrClassFile && se.Code == fileErrPositionInvalid
	}
	for _, tc := range []struct {
		name     string
		fail     string
		position uint32
		answer   func(error) bool
		open     int32
	}{
		{"skip succeeds", "", 2, func(err error) bool { return err == nil }, 1},
		{"read does not return normally", "Read", 2, operationalProblem, 0},
		{"position beyond the file", "", 10, positionInvalid, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := &openHandles{raisingFiles: &raisingFiles{memFileProvider: newMemFileProvider(), fail: tc.fail}}
			files.addFile("f", []byte("data"))
			srv, _, _ := limitServer(t, ServerOptions{FileProvider: files})
			c, _ := serveLoopback(t, srv, DialOptions{})
			defer func() { _ = c.Abort(context.Background()) }()
			_, err := c.FileOpen(ctx5s(t), "f", FileOpenOptions{InitialPosition: tc.position})
			if !tc.answer(err) {
				t.Errorf("FileOpen at %d = %v", tc.position, err)
			}
			if n := files.open.Load(); n != tc.open {
				t.Errorf("open handles after the FileOpen: %d, want %d", n, tc.open)
			}
		})
	}
}
