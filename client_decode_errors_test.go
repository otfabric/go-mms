// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

func TestProcessConfirmedPDU_Errors(t *testing.T) {
	c := &Client{}
	decode := func(full []byte) (pdu.PduKind, []byte) {
		t.Helper()
		kind, content, err := pdu.DecodePdu(full)
		if err != nil {
			t.Fatal(err)
		}
		return kind, content
	}
	resp, _ := codec.MarshalConfirmedResponse(8, asn1util.TagNumIdentify, true, nil)
	respKind, respContent := decode(resp)
	errKind, errContent := decode(codec.MarshalConfirmedError(8, 2, 1))
	rejKind, rejContent := decode(codec.MarshalRejectPDU(8, 1, 1))

	cases := []struct {
		name    string
		kind    pdu.PduKind
		content []byte
		want    error
	}{
		{"response garbage", pdu.PduConfirmedResponse, []byte{0xff}, ErrDecodeFailed},
		{"response invoke mismatch", respKind, respContent, ErrProtocol},
		{"error garbage", pdu.PduConfirmedError, []byte{0xff}, ErrDecodeFailed},
		{"error invoke mismatch", errKind, errContent, ErrProtocol},
		{"reject garbage", pdu.PduReject, []byte{0xff}, ErrDecodeFailed},
		{"reject invoke mismatch", rejKind, rejContent, ErrProtocol},
		{"unexpected kind", pdu.PduInitiateResponse, nil, ErrProtocol},
	}
	for _, tc := range cases {
		if _, err := c.processConfirmedPDU(9, tc.kind, tc.content); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// replyTransport answers the association request with a scripted SPDU.
type replyTransport struct{ reply []byte }

func (r *replyTransport) Send(context.Context, []byte) error { return nil }

func (r *replyTransport) Receive(context.Context) ([]byte, error) {
	if r.reply == nil {
		return nil, io.EOF
	}
	d := r.reply
	r.reply = nil
	return d, nil
}

func (r *replyTransport) Close() error { return nil }

func TestNewClient_BadAssociationResponse(t *testing.T) {
	identify, _ := pdu.MarshalIdentifyRequest(1)
	cases := []struct {
		name  string
		reply []byte
		want  error
	}{
		{"not a session SPDU", []byte{0xff, 0x00}, ErrDecodeFailed},
		{"no MMS payload", isostack.EncodeAssociateResponse(nil), ErrProtocol},
		{"undecodable MMS payload", isostack.EncodeAssociateResponse([]byte{0xff}), ErrDecodeFailed},
		{"not an InitiateResponse", isostack.EncodeAssociateResponse(identify), ErrProtocol},
		{"malformed InitiateResponse", isostack.EncodeAssociateResponse([]byte{asn1util.TagInitiateResponse, 0x02, 0xff, 0x00}), ErrDecodeFailed},
	}
	for _, tc := range cases {
		_, err := NewClient(context.Background(), &replyTransport{reply: tc.reply}, DialOptions{})
		if !errors.Is(err, tc.want) || !errors.Is(err, ErrAssociationFailed) {
			t.Errorf("%s: %v, want %v and ErrAssociationFailed", tc.name, err, tc.want)
		}
	}
}
