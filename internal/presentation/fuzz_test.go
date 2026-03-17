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

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
	})
}
