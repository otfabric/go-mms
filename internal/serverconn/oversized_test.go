// SPDX-License-Identifier: MIT

package serverconn

import (
	"bytes"
	"context"
	"math"
	"runtime"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
)

// allocated returns the octets fn allocates on the heap: the least of five
// measurements, since TotalAlloc also counts other goroutines.
func allocated(fn func()) uint64 {
	least := uint64(math.MaxUint64)
	for range 5 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		fn()
		runtime.ReadMemStats(&after)
		least = min(least, after.TotalAlloc-before.TotalAlloc)
	}
	return least
}

// nestedService is a service body of about size octets: an OCTET STRING
// inside depth constructed elements of indefinite length, an encoding a
// full decode rewrites level by level.
func nestedService(depth, size int) []byte {
	b := asn1util.WrapPrimitive(4, make([]byte, size))
	for range depth {
		b = append(append([]byte{0x30, 0x80}, b...), 0x00, 0x00)
	}
	return b
}

// A request larger than the negotiated PDU size is answered from its
// invokeID alone: the server allocates a small, fixed amount whatever the
// size and content of the PDU, and does not decode the rest of it.
func TestOversizedRequestAllocation(t *testing.T) {
	opts := defaultOpts()
	opts.MaxPDUSize = minPDUSize
	for _, size := range []int{32 << 10, 60 << 10} {
		req := mustConfirmedReq(t, 7, 4, nestedService(60, size))
		tr := &scriptTransport{}
		c := New(tr, discardLogger(), nopHandler, opts)
		const runs = 10
		alloc := allocated(func() {
			for range runs {
				if err := c.dispatchPDU(context.Background(), req); err != nil {
					t.Fatal(err)
				}
			}
		}) / runs
		if budget := uint64(len(req) / 16); alloc > budget {
			t.Errorf("an oversized request of %d octets allocates %d octets; budget %d", len(req), alloc, budget)
		}
		sent := tr.sentFrames()
		answer, err := isostack.DecodeDataResponse(sent[len(sent)-1])
		if want := codec.MarshalRejectPDU(7, RejectConfirmedRequest, ReasonValueOutOfRange); err != nil || !bytes.Equal(answer, want) {
			t.Errorf("answer = %x, %v; want %x", answer, err, want)
		}
	}
}
