// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// The go-mms encoder always sends a TimeOfDay millisecond field below one
// day, so these tests rewrite the server's bytes in transit to a form a
// peer may send instead.

var epoch1984 = time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC)

// timeOfDay6 is the 6-octet TimeOfDay (tag 0x8c, length 6) with the given
// millisecond field and day count since 1984-01-01.
func timeOfDay6(msOfDay uint32, days uint16) []byte {
	b := []byte{0x8c, 0x06, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[2:6], msOfDay)
	binary.BigEndian.PutUint16(b[6:8], days)
	return b
}

// canonicalTimeOfDay6 is the encoding go-mms sends for ms since the Unix
// epoch: a millisecond field below one day.
func canonicalTimeOfDay6(ms int64) []byte {
	since := ms - epoch1984.UnixMilli()
	day := int64(24 * time.Hour / time.Millisecond)
	return timeOfDay6(uint32(since%day), uint16(since/day))
}

// rewriteConn replaces from with to (same length) in the bytes it reads,
// so TPKT, COTP and BER lengths stay valid.
type rewriteConn struct {
	net.Conn
	from, to []byte
	hits     *atomic.Int32
}

func (c *rewriteConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if i := bytes.Index(p[:n], c.from); i >= 0 {
		copy(p[i:], c.to)
		c.hits.Add(1)
	}
	return n, err
}

// dialRewriting connects a client whose received bytes have from replaced
// by to.
func dialRewriting(t *testing.T, addr string, from, to []byte) (*mms.Client, *atomic.Int32) {
	t.Helper()
	hits := new(atomic.Int32)
	dialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &rewriteConn{Conn: conn, from: from, to: to, hits: hits}, nil
	}
	c, err := iso.Dial(timeout(t), addr, iso.WithDialer(dialer))
	if err != nil {
		t.Fatalf("iso.Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Abort(context.Background()) })
	return c, hits
}

// TestClient_BinaryTimeCarriedMillisecondOfDay reads a 6-octet TimeOfDay
// whose millisecond field holds one day more than midnight-relative (the
// day count one lower), which libiec61850, Wireshark, dscsystems and
// iec61850-rust all accept, and writes the value back.
func TestClient_BinaryTimeCarriedMillisecondOfDay(t *testing.T) {
	m := startModel(t)
	want := t0.UnixMilli() + 123 // dev/NewBinaryTime's initial value
	canonical := canonicalTimeOfDay6(want)
	msOfDay := binary.BigEndian.Uint32(canonical[2:6])
	days := binary.BigEndian.Uint16(canonical[6:8])
	carried := timeOfDay6(msOfDay+24*60*60*1000, days-1)

	c, hits := dialRewriting(t, m.addr, canonical, carried)
	ctx := timeout(t)
	got, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "NewBinaryTime"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if hits.Load() == 0 {
		t.Fatalf("the response did not carry %x; nothing was rewritten", canonical)
	}
	if ms, ok := got.Value.BinaryTime(); !ok || ms != want {
		t.Fatalf("BinaryTime = %d, %v; want %d (%s)", ms, ok, want, time.UnixMilli(want).UTC())
	}

	// The decoded value re-encodes: write it back and read the model.
	if _, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: "NewBinaryTime", Value: got.Value}); err != nil {
		t.Fatalf("Write back: %v", err)
	}
	mustEqual(t, "written back", m.get(dom+"/NewBinaryTime"), mms.NewBinaryTime(want))
}

// TestClient_BinaryTimeLeadingBitsSet reads a 6-octet TimeOfDay whose
// millisecond field has bit 28 set, which ISO 9506-2:2003 §7.5.1 requires
// to be zero: the client reports a decode failure and the association
// stays usable.
func TestClient_BinaryTimeLeadingBitsSet(t *testing.T) {
	m := startModel(t)
	canonical := canonicalTimeOfDay6(t0.UnixMilli() + 123)
	days := binary.BigEndian.Uint16(canonical[6:8])
	bad := timeOfDay6(1<<28, days)

	c, hits := dialRewriting(t, m.addr, canonical, bad)
	ctx := timeout(t)
	_, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "NewBinaryTime"})
	if hits.Load() == 0 {
		t.Fatalf("the response did not carry %x; nothing was rewritten", canonical)
	}
	if !errors.Is(err, mms.ErrDecodeFailed) {
		t.Fatalf("Read = %v, want ErrDecodeFailed", err)
	}
	if _, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "NewInteger"}); err != nil {
		t.Fatalf("Read after the decode failure: %v", err)
	}
}

// TestClientServer_BinaryTimeMillisecondFieldOverflow writes and reads
// back TimeOfDay values whose millisecond field is one day or more: a
// 4-octet deadline past midnight and a 6-octet instant after 2163-06-05,
// which goes out on the last day with the remainder in the field.
func TestClientServer_BinaryTimeMillisecondFieldOverflow(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	lastDay := epoch1984.AddDate(0, 0, 0xffff)
	for _, ms := range []int64{
		24*60*60*1000 + 90_000,                  // 4-octet: 00:01:30 tomorrow
		1<<28 - 1,                               // 4-octet: field maxed
		lastDay.Add(36 * time.Hour).UnixMilli(), // 6-octet: 2163-06-06T12:00Z
		lastDay.UnixMilli() + 1<<28 - 1,         // 6-octet: last instant of the form
	} {
		if _, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: "NewBinaryTime", Value: mms.NewBinaryTime(ms)}); err != nil {
			t.Fatalf("Write %d: %v", ms, err)
		}
		mustEqual(t, "written", m.get(dom+"/NewBinaryTime"), mms.NewBinaryTime(ms))
		got, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "NewBinaryTime"})
		if err != nil {
			t.Fatalf("Read %d: %v", ms, err)
		}
		if back, ok := got.Value.BinaryTime(); !ok || back != ms {
			t.Fatalf("Read back = %d, %v; want %d", back, ok, ms)
		}
	}
}
