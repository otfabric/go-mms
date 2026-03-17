package session

import "testing"

func FuzzSessionParse(f *testing.F) {
	// CONNECT SPDU
	f.Add(EncodeConnect(ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
		CalledSelector:  []byte{0x00, 0x01},
	}, []byte{0x31, 0x00}))

	// ACCEPT SPDU
	f.Add(EncodeAccept(ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
	}, []byte{0x31, 0x00}))

	// DATA SPDU
	f.Add(EncodeData([]byte{0x61, 0x00}))

	// FINISH SPDU
	f.Add(EncodeFinish([]byte{0x61, 0x00}))

	// DISCONNECT SPDU
	f.Add(EncodeDisconnect([]byte{0x61, 0x00}))

	// ABORT SPDU
	f.Add(EncodeAbort([]byte{0x64, 0x00}))

	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{0x01, 0x00, 0x01, 0x00}) // minimal DATA

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
	})
}
