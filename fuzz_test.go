// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

// FuzzValueCodec decodes an MMS Data element into a Value and checks that
// encoding the Value again gives the same Value back.
func FuzzValueCodec(f *testing.F) {
	for _, v := range []*Value{
		NewBoolean(true), NewInteger(-5), NewUnsigned(7), NewFloat(1.5), NewReal(2.25),
		NewVisibleString("abc"), NewMmsString("äö"), NewOctetString([]byte{1, 2}),
		NewBitStringWithLength([]byte{0xa0}, 3), NewBooleanArray([]byte{0x80}, 1),
		NewBCD(42), NewBinaryTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()), NewObjectIdentifier([]int{1, 2, 840}),
		NewGeneralizedTime(time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)),
		NewUTCTimeWithQuality(time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC), 0x0a),
		NewStructure([]*Value{NewInteger(1), NewArray([]*Value{NewBoolean(false)})}),
	} {
		dv, err := valueToDataValue(v)
		if err != nil {
			f.Fatalf("seed %s: %v", v, err)
		}
		enc, err := pdu.MarshalData(dv)
		if err != nil {
			f.Fatalf("seed %s: %v", v, err)
		}
		f.Add(enc)
	}
	f.Add([]byte{0xa2, 0x80, 0x00, 0x00}) // indefinite length
	f.Add([]byte{0x85, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		dv, _, err := pdu.UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		v, err := dataValueToValue(dv)
		if err != nil {
			return
		}
		_ = v.String()
		if !v.Clone().Equal(v) {
			t.Fatalf("Clone differs: %s", v)
		}
		dv2, err := valueToDataValue(v)
		if err != nil {
			return // e.g. a DataAccessError value is not writable
		}
		enc, err := pdu.MarshalData(dv2)
		if err != nil {
			t.Fatalf("re-encode %s: %v", v, err)
		}
		dv3, _, err := pdu.UnmarshalDataElement(enc, 0)
		if err != nil {
			t.Fatalf("decode re-encoded %x: %v", enc, err)
		}
		v3, err := dataValueToValue(dv3)
		if err != nil {
			t.Fatalf("convert re-encoded %x: %v", enc, err)
		}
		if !v3.Equal(v) {
			t.Fatalf("round trip: %s != %s", v3, v)
		}
	})
}

// fuzzServer builds a server with variables of several types, an NVL and
// association-scope state.
func fuzzServer(tb testing.TB) *Server {
	tb.Helper()
	s := NewServer(ServerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	s.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "v"}, nil
	})
	s.HandleStatus(func(context.Context, StatusRequest) (*ServerStatus, error) {
		return &ServerStatus{}, nil
	})
	if err := s.RegisterDomain("LD"); err != nil {
		tb.Fatal(err)
	}
	i32 := TypeSpec{Type: ValueTypeInteger, Size: 32}
	rec := TypeSpec{Type: ValueTypeStructure, Elements: []TypeSpecElement{
		{Name: "a", Type: i32},
		{Name: "b", Type: TypeSpec{Type: ValueTypeArray, Count: 3, Element: &i32}},
	}}
	vars := []Variable{
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "int"}, TypeSpec: i32, Read: constRead(NewInteger(1)), Write: nopWrite},
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "rec"}, TypeSpec: rec, Read: constRead(rec.DefaultValue()), Write: nopWrite},
		{Name: ObjectName{Scope: ObjectScopeVMD, ItemID: "vmd"}, TypeSpec: i32, Read: constRead(NewInteger(2))},
	}
	for _, v := range vars {
		if err := s.RegisterVariable(v); err != nil {
			tb.Fatal(err)
		}
	}
	if err := s.RegisterNamedVariableList(NamedVariableList{
		Name:      ObjectName{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "nvl"},
		Variables: []VariableSpec{{Name: vars[0].Name}, {Name: vars[1].Name}},
	}); err != nil {
		tb.Fatal(err)
	}
	return s
}

func constRead(v *Value) func(context.Context) (*Value, error) {
	return func(context.Context) (*Value, error) { return v, nil }
}

func nopWrite(context.Context, *Value) error { return nil }

// FuzzServerDispatch feeds arbitrary service bodies to every confirmed
// service handler. Handlers must answer with a response or a service
// error, never panic.
func FuzzServerDispatch(f *testing.F) {
	readReq := readSeed(f)
	for tag, body := range map[int][]byte{
		asn1util.TagNumIdentify:    nil,
		asn1util.TagNumStatus:      {0x00},
		asn1util.TagNumRead:        readReq,
		asn1util.TagNumWrite:       {0xa0, 0x00, 0xa0, 0x00},
		asn1util.TagNumGetNameList: {0xa0, 0x03, 0x80, 0x01, 0x00, 0xa1, 0x02, 0x80, 0x00},
		asn1util.TagNumFileOpen:    {0xa0, 0x03, 0x19, 0x01, 0x61, 0x81, 0x01, 0x00},
		99:                         {0x00},
	} {
		f.Add(tag, body)
	}

	s := fuzzServer(f)
	f.Fuzz(func(t *testing.T, tag int, body []byte) {
		sc := &ServerConn{busy: make(chan struct{}, 1), frsmTable: newFRSMTable()}
		ctx := context.WithValue(context.Background(), serverConnCtxKey{}, sc)
		_, _, _, err := s.dispatch(ctx, 1, tag, body)
		_ = toWireServiceError(err)
	})
}

// fuzzTransport replays scripted session SPDUs and discards what is sent.
type fuzzTransport struct {
	in [][]byte
}

func (f *fuzzTransport) Send(context.Context, []byte) error { return nil }

func (f *fuzzTransport) Receive(context.Context) ([]byte, error) {
	if len(f.in) == 0 {
		return nil, io.EOF
	}
	d := f.in[0]
	f.in = f.in[1:]
	return d, nil
}

func (f *fuzzTransport) Close() error { return nil }

// FuzzServerSession runs the whole server receive path — session,
// presentation and MMS decoding, dispatch — on an arbitrary SPDU received
// after a valid association.
func FuzzServerSession(f *testing.F) {
	initBytes, err := pdu.MarshalInitiateRequest(pdu.DefaultInitiateRequest(0, 0, 0, 0))
	if err != nil {
		f.Fatal(err)
	}
	aarq, err := isostack.EncodeAssociateRequest(isostack.Params{ACSE: acse.AARQParams{}}, initBytes)
	if err != nil {
		f.Fatal(err)
	}
	identify, _ := pdu.MarshalIdentifyRequest(1)
	f.Add(isostack.EncodeDataRequest(identify))
	f.Add(isostack.EncodeDataRequest(codec.MarshalConcludeRequest()))
	f.Add(isostack.EncodeDataRequest(asn1util.WrapPrimitive(5, berutil.EncodeUint32(3))))
	f.Add(isostack.EncodeReleaseRequest())
	f.Add(isostack.EncodeAbort())
	f.Add([]byte{0x01, 0x00, 0x01, 0x00, 0x61, 0x00})

	s := fuzzServer(f)
	f.Fuzz(func(t *testing.T, spdu []byte) {
		tr := &fuzzTransport{in: [][]byte{aarq, spdu, spdu}}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.Serve(ctx, tr)
		if err != nil && !errors.Is(err, ErrClosed) {
			t.Fatalf("Serve = %v, want nil or ErrClosed", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Serve hung on %x", spdu)
		}
	})
}

// readSeed returns the service body of a Read of LD/rec.
func readSeed(tb testing.TB) []byte {
	tb.Helper()
	full, err := pdu.MarshalReadRequest(1, []pdu.ObjectNameWire{{Scope: pdu.ScopeDomain, DomainID: "LD", ItemID: "rec"}})
	if err != nil {
		tb.Fatal(err)
	}
	_, content, err := pdu.DecodePdu(full)
	if err != nil {
		tb.Fatal(err)
	}
	_, raw, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		tb.Fatal(err)
	}
	return raw.Bytes
}

// Ensure the dispatch fuzz seeds stay valid encodings.
func TestFuzzSeedsDecode(t *testing.T) {
	readReq := readSeed(t)
	s := fuzzServer(t)
	sc := &ServerConn{busy: make(chan struct{}, 1), frsmTable: newFRSMTable()}
	ctx := context.WithValue(context.Background(), serverConnCtxKey{}, sc)
	tag, _, resp, err := s.dispatch(ctx, 1, asn1util.TagNumRead, readReq)
	if err != nil || tag != asn1util.TagNumRead || !bytes.Contains(resp, []byte{0xa2}) {
		t.Fatalf("Read seed: tag=%d resp=%x err=%v", tag, resp, err)
	}
}
