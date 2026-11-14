// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

// minNegotiatedPDUSize is the smallest localDetailCalling the server
// accepts (internal/serverconn).
const minNegotiatedPDUSize = 64

// The answers of a fixed size fit the smallest PDU size the server
// negotiates: the largest FileOpen-Response, and a FileRead-Response
// with one octet of data.
func TestMinimumPDUSizeFitsFixedAnswers(t *testing.T) {
	open, err := pdu.MarshalFileOpenResponse(math.MaxInt32, math.MaxUint32, time.Date(2106, 2, 7, 6, 28, 15, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) > 32 {
		t.Errorf("largest FileOpen-Response payload %d octets, documented 32", len(open))
	}
	read, err := pdu.MarshalFileReadResponse([]byte{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]struct {
		tag     int
		payload []byte
	}{
		"FileOpen": {asn1util.TagNumFileOpen, open},
		"FileRead": {asn1util.TagNumFileRead, read},
	} {
		b, err := codec.MarshalConfirmedResponse(codec.MaxInvokeID, a.tag, true, a.payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > minNegotiatedPDUSize || len(a.payload) > minNegotiatedPDUSize-confirmedResponseOverhead {
			t.Errorf("%s-Response: %d octets (payload %d), minimum PDU size %d", name, len(b), len(a.payload), minNegotiatedPDUSize)
		}
	}
	// The FileRead budget leaves at least one octet at the minimum.
	if n := minNegotiatedPDUSize - confirmedResponseOverhead - 8; n < 1 {
		t.Errorf("file data budget at the minimum PDU size: %d", n)
	}
}

// recordingFileProvider records what the server asks of a memFileProvider.
type recordingFileProvider struct {
	*memFileProvider
	mu          sync.Mutex
	open        int // handles opened and not closed
	minMaxBytes int
	reads       int
}

func (p *recordingFileProvider) Open(ctx context.Context, path string) (FileHandle, FileAttributes, error) {
	h, a, err := p.memFileProvider.Open(ctx, path)
	if err == nil {
		p.mu.Lock()
		p.open++
		p.mu.Unlock()
	}
	a.LastModified = time.Time{} // the smallest FileOpen-Response
	return h, a, err
}

func (p *recordingFileProvider) Close(ctx context.Context, h FileHandle) error {
	p.mu.Lock()
	p.open--
	p.mu.Unlock()
	return p.memFileProvider.Close(ctx, h)
}

func (p *recordingFileProvider) Read(ctx context.Context, h FileHandle, maxBytes int) ([]byte, bool, error) {
	p.mu.Lock()
	p.reads++
	if p.reads == 1 || maxBytes < p.minMaxBytes {
		p.minMaxBytes = maxBytes
	}
	p.mu.Unlock()
	if maxBytes < 1 {
		return nil, true, nil
	}
	return p.memFileProvider.Read(ctx, h, maxBytes)
}

func (p *recordingFileProvider) state() (open, reads, minMaxBytes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.open, p.reads, p.minMaxBytes
}

// fileServerWithPDUSize serves fp with the server's own MaxPDUSize and
// returns a client and the channel of Serve's result.
func fileServerWithPDUSize(t *testing.T, maxPDU int, fp FileProvider) (*Client, <-chan error) {
	t.Helper()
	srv := NewServer(ServerOptions{FileProvider: fp, MMS: ServerMMSOptions{MaxPDUSize: maxPDU}})
	c, served := serveLoopback(t, srv, DialOptions{})
	t.Cleanup(func() { _ = c.Abort(context.Background()); _ = waitServe(t, served) })
	return c, served
}

func isPDUSizeError(err error) bool {
	se, ok := errors.AsType[*ServiceError](err)
	return ok && se.Class == ErrorClassService && se.Code == svcErrPDUSize
}

// A negotiated PDU size without room for file data is answered with
// ServiceError service pdu-size; the provider is never asked for fewer
// than one octet.
func TestFileReadWithoutRoomForData(t *testing.T) {
	fp := &recordingFileProvider{memFileProvider: newMemFileProvider()}
	fp.addFile("f", []byte("data"))
	// 24 octets leave room for the FileOpen-Response of a small file,
	// none for file data (16 + 8 octets of framing).
	c, _ := fileServerWithPDUSize(t, 24, fp)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if got := c.Negotiated().MaxPDUSize; got != 24 {
		t.Fatalf("negotiated %d", got)
	}
	open, err := c.FileOpen(ctx, "f")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}
	for range 3 {
		if _, err := c.FileRead(ctx, open.FrsmID); !isPDUSizeError(err) {
			t.Fatalf("FileRead = %v, want ServiceError service pdu-size", err)
		}
	}
	if _, reads, minMax := fp.state(); reads > 0 && minMax < 1 {
		t.Fatalf("provider asked for %d octets (%d reads)", minMax, reads)
	}
}

// A FileOpen whose answer does not fit the negotiated PDU size leaves no
// handle open on the server.
func TestFileOpenResponseTooLargeClosesHandle(t *testing.T) {
	fp := &recordingFileProvider{memFileProvider: newMemFileProvider()}
	fp.addFile("f", []byte("data"))
	c, _ := fileServerWithPDUSize(t, 20, fp)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.FileOpen(ctx, "f"); !isPDUSizeError(err) {
		t.Fatalf("FileOpen = %v, want ServiceError service pdu-size", err)
	}
	if open, _, _ := fp.state(); open != 0 {
		t.Fatalf("%d handles open after the refused FileOpen", open)
	}
	// The identifier the server would have assigned names no file.
	if _, err := c.FileRead(ctx, 1); err == nil {
		t.Fatal("FileRead of the refused handle succeeded")
	}
}

// presentationContextConnect is a CONNECT whose CP proposes n contexts
// the server does not know, and carries an AARQ.
func presentationContextConnect(t *testing.T, n int) []byte {
	t.Helper()
	aarq, err := acse.EncodeAARQ(acse.AARQParams{}, []byte{0xa8, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	def := berutil.EncodeTLV(0x30, append(berutil.EncodeTLV(0x02, []byte{5}), 0x06, 0x00))
	var list []byte
	for range n {
		list = append(list, def...)
	}
	pdv := berutil.EncodeTLV(0x30, append(berutil.EncodeTLV(0x02, []byte{1}), berutil.EncodeTLV(0xa0, aarq)...))
	normal := append(berutil.EncodeTLV(0xa4, list), berutil.EncodeTLV(0x61, pdv)...)
	cp := berutil.EncodeTLV(0x31, append(berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01}), berutil.EncodeTLV(0xa2, normal)...))
	pi := func(buf []byte, code byte, v []byte) []byte {
		if len(v) < 255 {
			buf = append(buf, code, byte(len(v)))
		} else {
			buf = append(buf, code, 0xff, byte(len(v)>>8), byte(len(v)))
		}
		return append(buf, v...)
	}
	var params []byte
	params = pi(params, 0x05, []byte{0x13, 0x01, 0x00, 0x16, 0x01, 0x02})
	params = pi(params, 0x14, []byte{0x00, 0x02})
	params = pi(params, 0xc2, cp)
	return pi(nil, 0x0d, params)
}

// A CONNECT proposing more presentation contexts than the server answers
// is refused: Serve returns ErrAssociationFailed and sends a REFUSE.
func TestServeRefusesPresentationContextList(t *testing.T) {
	for _, n := range []int{17, 8400} {
		srv := NewServer(ServerOptions{})
		cl, sr := loopbackPair()
		served := make(chan error, 1)
		go func() { served <- srv.Serve(context.Background(), sr) }()
		if err := cl.Send(context.Background(), presentationContextConnect(t, n)); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resp, err := cl.Receive(ctx)
		cancel()
		if err != nil || len(resp) == 0 || resp[0] != 0x0c {
			t.Fatalf("%d contexts: answer %x, %v; want a REFUSE", n, resp, err)
		}
		if err := waitServe(t, served); !errors.Is(err, ErrAssociationFailed) {
			t.Fatalf("%d contexts: Serve = %v, want ErrAssociationFailed", n, err)
		}
		_ = cl.Close()
	}
}
