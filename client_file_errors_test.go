// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
)

// fileJournalCalls invokes Identify, Status and each file and journal
// client API once.
var fileJournalCalls = []struct {
	name string
	tag  int
	call func(context.Context, *Client) error
}{
	{"Identify", asn1util.TagNumIdentify, func(ctx context.Context, c *Client) error {
		_, err := c.Identify(ctx)
		return err
	}},
	{"Status", asn1util.TagNumStatus, func(ctx context.Context, c *Client) error {
		_, err := c.Status(ctx)
		return err
	}},
	{"FileOpen", asn1util.TagNumFileOpen, func(ctx context.Context, c *Client) error {
		_, err := c.FileOpen(ctx, "f")
		return err
	}},
	{"FileRead", asn1util.TagNumFileRead, func(ctx context.Context, c *Client) error {
		_, err := c.FileRead(ctx, 1)
		return err
	}},
	{"FileClose", asn1util.TagNumFileClose, func(ctx context.Context, c *Client) error {
		return c.FileClose(ctx, 1)
	}},
	{"FileDelete", asn1util.TagNumFileDelete, func(ctx context.Context, c *Client) error {
		return c.FileDelete(ctx, "f")
	}},
	{"FileRename", asn1util.TagNumFileRename, func(ctx context.Context, c *Client) error {
		return c.FileRename(ctx, "a", "b")
	}},
	{"ObtainFile", asn1util.TagNumObtainFile, func(ctx context.Context, c *Client) error {
		return c.ObtainFile(ctx, "a", "b")
	}},
	{"FileDirectory", asn1util.TagNumFileDirectory, func(ctx context.Context, c *Client) error {
		_, err := c.FileDirectory(ctx, FileDirectoryRequest{})
		return err
	}},
	{"FileDirectoryAll", asn1util.TagNumFileDirectory, func(ctx context.Context, c *Client) error {
		_, err := c.FileDirectoryAll(ctx, "")
		return err
	}},
	{"ReadJournalTimeRange", asn1util.TagNumReadJournal, func(ctx context.Context, c *Client) error {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		_, err := c.ReadJournalTimeRange(ctx, "d", "j", start, start.Add(time.Hour))
		return err
	}},
	{"ReadJournalStartAfter", asn1util.TagNumReadJournal, func(ctx context.Context, c *Client) error {
		_, err := c.ReadJournalStartAfter(ctx, "d", "j", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), []byte{1})
		return err
	}},
}

// A response for the wrong service is a protocol error.
func TestFileJournal_WrongServiceResponse(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	for _, tc := range fileJournalCalls {
		go func() {
			id, _, _ := srv.handleDataRequest(ctx)
			resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumGetNameList, true, nil)
			srv.sendDataResponse(ctx, resp)
		}()
		err := tc.call(ctx, client)
		var pe *ProtocolError
		if !errors.As(err, &pe) || pe.Phase != PhaseMMS {
			t.Errorf("%s with a GetNameList response = %v, want ProtocolError in PhaseMMS", tc.name, err)
		}
	}
}

// A malformed body for the right service fails to decode; services with
// a NULL response accept any body.
func TestFileJournal_MalformedResponse(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	nullResponse := map[string]bool{"FileClose": true, "FileDelete": true, "FileRename": true, "ObtainFile": true}
	for _, tc := range fileJournalCalls {
		go func() {
			id, _, _ := srv.handleDataRequest(ctx)
			resp, _ := codec.MarshalConfirmedResponse(id, tc.tag, true, []byte{0xff, 0x05, 0x01})
			srv.sendDataResponse(ctx, resp)
		}()
		err := tc.call(ctx, client)
		if nullResponse[tc.name] {
			continue
		}
		if !errors.Is(err, ErrDecodeFailed) {
			t.Errorf("%s with a malformed response = %v, want ErrDecodeFailed", tc.name, err)
		}
	}
}

// Times outside the BinaryTime range are rejected before anything is sent.
func TestReadJournal_InvalidTimes(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	tooEarly := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := client.ReadJournalTimeRange(ctx, "d", "j", tooEarly, tooEarly); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ReadJournalTimeRange = %v, want ErrInvalidArgument", err)
	}
	if _, err := client.ReadJournalStartAfter(ctx, "d", "j", tooEarly, []byte{1}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ReadJournalStartAfter = %v, want ErrInvalidArgument", err)
	}
}
