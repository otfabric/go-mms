// SPDX-License-Identifier: MIT

package presentation

import "testing"

func FuzzPresentationParse(f *testing.F) {
	// CP PPDU
	f.Add(EncodeCP(ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
		CalledSelector:  []byte{0x00, 0x01},
	}, []byte{0x60, 0x00}))

	// CPA PPDU
	f.Add(EncodeCPA([]byte{0x00, 0x01}, []byte{0x61, 0x00}))

	// User data PPDU (MMS context)
	f.Add(EncodeUserData(ContextIDMMS, []byte{0xa1, 0x00}))

	// User data PPDU (ACSE context)
	f.Add(EncodeUserData(ContextIDACSE, []byte{0x62, 0x00}))

	f.Add([]byte{})
	f.Add([]byte{0xff})

	// CPR, octet-aligned user data
	f.Add(EncodeCPR([]byte{0, 1}, []ContextResult{{Result: 0, TransferSyntax: oidBER, ProviderReason: -1}}, -1, 1, []byte{0x61, 0x00}))
	f.Add([]byte{0x61, 0x0a, 0x30, 0x08, 0x02, 0x01, 0x03, 0x81, 0x03, 0xa1, 0x01, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err == nil && p.Kind == PpduCP {
			_ = Negotiate(p.Contexts)
		}
	})
}

func FuzzPresentationParseAbort(f *testing.F) {
	f.Add(EncodeARU(ContextIDACSE, []byte{0x64, 0x03, 0x80, 0x01, 0x00}))
	f.Add([]byte{0x30, 0x03, 0x80, 0x01, 0x01})
	f.Add([]byte{0xa0, 0x00})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseAbort(data)
	})
}
