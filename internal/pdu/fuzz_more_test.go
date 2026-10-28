// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"encoding/asn1"
	"reflect"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// serviceBody extracts the service request body (the bytes a server
// handler receives) from a complete ConfirmedRequest PDU.
func serviceBody(tb testing.TB, pduBytes []byte, err error) []byte {
	tb.Helper()
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	_, content, err := codec.UnwrapPdu(pduBytes)
	if err != nil {
		tb.Fatalf("UnwrapPdu: %v", err)
	}
	_, svc, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		tb.Fatalf("UnmarshalConfirmedRequest: %v", err)
	}
	return svc.Bytes
}

// addWithMutations adds a seed plus a truncated and a corrupted copy.
func addWithMutations(f *testing.F, args func(b []byte) []any, seeds ...[]byte) {
	for _, s := range seeds {
		f.Add(args(s)...)
		if len(s) > 1 {
			f.Add(args(s[:len(s)-1])...)
			c := bytes.Clone(s)
			c[len(c)/2] ^= 0x5a
			f.Add(args(c)...)
		}
	}
	for _, s := range [][]byte{{}, {0x00}, {0xff}, {0x30, 0x80}, {0xa0, 0x81}, {0x1a, 0x01}} {
		f.Add(args(s)...)
	}
}

func normBits(b asn1.BitString) asn1.BitString {
	if len(b.Bytes) == 0 {
		b.Bytes = nil
	}
	return b
}

func FuzzUnmarshalInitiateRequest(f *testing.F) {
	var seeds [][]byte
	for _, req := range []InitiateRequest{
		DefaultInitiateRequest(65000, 5, 5, 10),
		DefaultInitiateRequest(128, 1, 1, 1),
		{ProposedMaxServOutstandingCall: 1, ProposedMaxServOutstandingCalled: 1},
	} {
		b, err := MarshalInitiateRequest(req)
		if err != nil {
			f.Fatal(err)
		}
		_, content, err := codec.UnwrapPdu(b)
		if err != nil {
			f.Fatal(err)
		}
		// Also the explicit inner SEQUENCE form some peers send.
		seeds = append(seeds, content, berutil.EncodeTLV(0x30, content))
	}
	addWithMutations(f, func(b []byte) []any { return []any{b} }, seeds...)

	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := UnmarshalInitiateRequest(data)
		if err != nil {
			return
		}
		enc, err := MarshalInitiateRequest(*req)
		if err != nil {
			t.Fatalf("MarshalInitiateRequest(%+v): %v", *req, err)
		}
		kind, content, err := DecodePdu(enc)
		if err != nil || kind != PduInitiateRequest {
			t.Fatalf("DecodePdu(re-encoded) = %s, %v", kind, err)
		}
		again, err := UnmarshalInitiateRequest(content)
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v\noriginal %x\nre-encoded %x", err, data, content)
		}
		a, b := *req, *again
		a.InitRequestDetail.ProposedParamCBB = normBits(a.InitRequestDetail.ProposedParamCBB)
		b.InitRequestDetail.ProposedParamCBB = normBits(b.InitRequestDetail.ProposedParamCBB)
		a.InitRequestDetail.ServicesSupportedCalling = normBits(a.InitRequestDetail.ServicesSupportedCalling)
		b.InitRequestDetail.ServicesSupportedCalling = normBits(b.InitRequestDetail.ServicesSupportedCalling)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", b, a)
		}
	})
}

func FuzzUnmarshalInitiateResponse(f *testing.F) {
	var seeds [][]byte
	for _, resp := range []InitiateResponse{
		{
			LocalDetailCalled:                  65000,
			NegotiatedMaxServOutstandingCall:   5,
			NegotiatedMaxServOutstandingCalled: 5,
			NegotiatedDataStructureNesting:     10,
			InitResponseDetail: InitResponseDetail{
				NegotiatedVersion:       1,
				NegotiatedParamCBB:      asn1.BitString{Bytes: []byte{0xf1, 0x00}, BitLength: 11},
				ServicesSupportedCalled: asn1.BitString{Bytes: []byte{0xee, 0x1c, 0x00}, BitLength: 24},
			},
		},
		{NegotiatedMaxServOutstandingCall: 1, NegotiatedMaxServOutstandingCalled: 1},
	} {
		b, err := codec.MarshalMmsPduBareSequence(asn1util.TagInitiateResponse, resp)
		if err != nil {
			f.Fatal(err)
		}
		_, content, err := codec.UnwrapPdu(b)
		if err != nil {
			f.Fatal(err)
		}
		seeds = append(seeds, content)
	}
	addWithMutations(f, func(b []byte) []any { return []any{b} }, seeds...)

	f.Fuzz(func(t *testing.T, data []byte) {
		resp, err := UnmarshalInitiateResponse(data)
		if err != nil {
			return
		}
		enc, err := codec.MarshalMmsPduBareSequence(asn1util.TagInitiateResponse, *resp)
		if err != nil {
			t.Fatalf("marshal(%+v): %v", *resp, err)
		}
		kind, content, err := DecodePdu(enc)
		if err != nil || kind != PduInitiateResponse {
			t.Fatalf("DecodePdu(re-encoded) = %s, %v", kind, err)
		}
		again, err := UnmarshalInitiateResponse(content)
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v\noriginal %x\nre-encoded %x", err, data, content)
		}
		a, b := *resp, *again
		a.InitResponseDetail.NegotiatedParamCBB = normBits(a.InitResponseDetail.NegotiatedParamCBB)
		b.InitResponseDetail.NegotiatedParamCBB = normBits(b.InitResponseDetail.NegotiatedParamCBB)
		a.InitResponseDetail.ServicesSupportedCalled = normBits(a.InitResponseDetail.ServicesSupportedCalled)
		b.InitResponseDetail.ServicesSupportedCalled = normBits(b.InitResponseDetail.ServicesSupportedCalled)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", b, a)
		}
	})
}

// File service request decoders, selected by the first fuzz argument.
const (
	fileReqRead = iota
	fileReqClose
	fileReqDelete
	fileReqRename
	fileReqObtain
	numFileReqs
)

func FuzzUnmarshalFileRequests(f *testing.F) {
	type seed struct {
		kind uint8
		body []byte
	}
	var seeds []seed
	add := func(kind uint8, b []byte, err error) {
		seeds = append(seeds, seed{kind, serviceBody(f, b, err)})
	}
	for _, id := range []int32{0, 1, 127, 128, -1, 1 << 30} {
		b, err := MarshalFileReadRequest(1, id)
		add(fileReqRead, b, err)
		b, err = MarshalFileCloseRequest(1, id)
		add(fileReqClose, b, err)
	}
	for _, name := range []string{"", "a", "COMTRADE/rec_0001.cfg", string(bytes.Repeat([]byte("x"), 200))} {
		b, err := MarshalFileDeleteRequest(1, name)
		add(fileReqDelete, b, err)
		b, err = MarshalFileRenameRequest(1, name, name+".bak")
		add(fileReqRename, b, err)
		b, err = MarshalObtainFileRequest(1, name, "dst/"+name)
		add(fileReqObtain, b, err)
	}
	for _, s := range seeds {
		addWithMutations(f, func(b []byte) []any { return []any{s.kind, b} }, s.body)
	}

	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		switch kind % numFileReqs {
		case fileReqRead, fileReqClose:
			dec, marshal := UnmarshalFileReadRequest, MarshalFileReadRequest
			if kind%numFileReqs == fileReqClose {
				dec, marshal = UnmarshalFileCloseRequest, MarshalFileCloseRequest
			}
			id, err := dec(data)
			if err != nil {
				return
			}
			b, err := marshal(1, id)
			again, err := dec(serviceBody(t, b, err))
			if err != nil || again != id {
				t.Fatalf("frsmID round-trip: %d -> %d (%v)", id, again, err)
			}
		case fileReqDelete:
			name, err := UnmarshalFileDeleteRequest(data)
			if err != nil {
				return
			}
			b, err := MarshalFileDeleteRequest(1, name)
			again, err := UnmarshalFileDeleteRequest(serviceBody(t, b, err))
			if err != nil || again != name {
				t.Fatalf("file name round-trip: %q -> %q (%v)", name, again, err)
			}
		case fileReqRename:
			req, err := UnmarshalFileRenameRequest(data)
			if err != nil {
				return
			}
			b, err := MarshalFileRenameRequest(1, req.CurrentFileName, req.NewFileName)
			again, err := UnmarshalFileRenameRequest(serviceBody(t, b, err))
			if err != nil || *again != *req {
				t.Fatalf("rename round-trip: %+v -> %+v (%v)", req, again, err)
			}
		case fileReqObtain:
			req, err := UnmarshalObtainFileRequest(data)
			if err != nil {
				return
			}
			b, err := MarshalObtainFileRequest(1, req.SourceFile, req.DestinationFile)
			again, err := UnmarshalObtainFileRequest(serviceBody(t, b, err))
			if err != nil || *again != *req {
				t.Fatalf("obtain-file round-trip: %+v -> %+v (%v)", req, again, err)
			}
		}
	})
}

// Variable-access and named-variable-list request decoders.
const (
	nvlReqGetVarAccess = iota
	nvlReqGetNVLAttrs
	nvlReqDeleteNVL
	numNVLReqs
)

func FuzzUnmarshalNVLRequests(f *testing.F) {
	names := []ObjectNameWire{
		{Scope: 0, ItemID: "vmdVar"},
		{Scope: 1, DomainID: "IED1LD0", ItemID: "LLN0$ST$Mod$stVal"},
		{Scope: 2, ItemID: "aaVar"},
	}
	type seed struct {
		kind uint8
		body []byte
	}
	var seeds []seed
	add := func(kind uint8, b []byte, err error) {
		seeds = append(seeds, seed{kind, serviceBody(f, b, err)})
	}
	for _, n := range names {
		b, err := MarshalGetVarAccessRequest(1, n)
		add(nvlReqGetVarAccess, b, err)
		b, err = MarshalGetNamedVarListAttrsRequest(1, n)
		add(nvlReqGetNVLAttrs, b, err)
	}
	b, err := MarshalDeleteNamedVarListRequest(1, names)
	add(nvlReqDeleteNVL, b, err)
	b, err = MarshalDeleteNVLDomainScopeRequest(1, "IED1LD0")
	add(nvlReqDeleteNVL, b, err)
	b, err = MarshalDeleteNVLVMDScopeRequest(1)
	add(nvlReqDeleteNVL, b, err)
	for _, s := range seeds {
		addWithMutations(f, func(b []byte) []any { return []any{s.kind, b} }, s.body)
	}

	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		switch kind % numNVLReqs {
		case nvlReqGetVarAccess:
			name, err := UnmarshalGetVarAccessRequest(data)
			if err != nil {
				return
			}
			b, err := MarshalGetVarAccessRequest(1, name)
			again, err := UnmarshalGetVarAccessRequest(serviceBody(t, b, err))
			if err != nil || again != name {
				t.Fatalf("GetVarAccess round-trip: %+v -> %+v (%v)", name, again, err)
			}
		case nvlReqGetNVLAttrs:
			req, err := UnmarshalGetNVLAttrsRequest(data)
			if err != nil {
				return
			}
			b, err := MarshalGetNamedVarListAttrsRequest(1, req.ListName)
			again, err := UnmarshalGetNVLAttrsRequest(serviceBody(t, b, err))
			if err != nil || *again != *req {
				t.Fatalf("GetNVLAttrs round-trip: %+v -> %+v (%v)", req, again, err)
			}
		case nvlReqDeleteNVL:
			req, err := UnmarshalDeleteNVLRequest(data)
			if err != nil || req.ScopeOfDelete != 0 || len(req.ListNames) == 0 {
				return
			}
			b, err := MarshalDeleteNamedVarListRequest(1, req.ListNames)
			again, err := UnmarshalDeleteNVLRequest(serviceBody(t, b, err))
			if err != nil || !reflect.DeepEqual(again.ListNames, req.ListNames) {
				t.Fatalf("DeleteNVL round-trip: %+v -> %+v (%v)", req, again, err)
			}
		}
	})
}
